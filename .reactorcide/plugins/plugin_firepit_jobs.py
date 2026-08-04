"""Trusted runnerlib jobs for Firepit."""

from __future__ import annotations

import base64
import json
import os
import platform
import re
import shutil
import subprocess
import sys
import tarfile
import tempfile
import time
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path
from typing import Any, Mapping, Sequence

from src.logging import log_stdout
from src.plugins import Plugin, PluginContext, PluginPhase


GO_VERSION = "1.26.4"
NODE_VERSION = "26.1.0"
SEMVER_TAGS_VERSION = "v0.6.0"
BUILDKIT_VERSION = "0.17.3"
HELM_VERSION = "v3.18.4"
KUBECTL_VERSION = "v1.33.2"
GITHUB_API_VERSION = "2022-11-28"
RELEASE_TAG = re.compile(
    r"^v(?P<version>(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\."
    r"(?:0|[1-9][0-9]*)(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?)$"
)
CONVENTIONAL_COMMIT = re.compile(
    r"^(feat|fix|docs|style|refactor|perf|test|build|ci|chore|revert)"
    r"(\([^)]+\))?!?: .+"
)
REPOSITORY = re.compile(r"^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$")
RELEASE_MARKER_PREFIX = "<!-- firepit-release-source:"
SECRET_NAMES = (
    "GITHUB_PAT",
    "REGISTRY_PASSWORD",
    "KUBECONFIG_CONTENT",
    "LINKKEYS_PKI_API_KEY",
    "LINKKEYS_RP_DOMAIN_KEY_PASSPHRASE",
)


def _repo_root(context: PluginContext) -> Path:
    configured = Path(context.config.code_dir)
    if configured.exists():
        return configured.resolve()
    source_path = context.metadata.get("source_path")
    if source_path:
        return Path(source_path).resolve()
    return Path("/job/src")


def _environment() -> dict[str, str]:
    environment = os.environ.copy()
    for name in SECRET_NAMES:
        environment.pop(name, None)
    return environment


def _run(
    args: Sequence[str | Path],
    *,
    cwd: Path,
    env: Mapping[str, str] | None = None,
    capture: bool = False,
    input_text: str | None = None,
    sensitive: Sequence[str] = (),
) -> subprocess.CompletedProcess[str]:
    command = tuple(str(arg) for arg in args)
    printable = " ".join(command)
    for value in sensitive:
        if value:
            printable = printable.replace(value, "[REDACTED]")
    log_stdout(f"+ {printable}")
    command_environment = _environment()
    if env:
        command_environment.update(env)
    return subprocess.run(
        command,
        cwd=cwd,
        env=command_environment,
        check=True,
        shell=False,
        text=True,
        capture_output=capture,
        input=input_text,
    )


def _required(name: str) -> str:
    value = os.environ.get(name, "").strip()
    if not value:
        raise RuntimeError(f"{name} is required")
    return value


def _architecture() -> tuple[str, str]:
    machine = platform.machine()
    values = {
        "aarch64": ("arm64", "arm64"),
        "x86_64": ("amd64", "x64"),
    }
    if machine not in values:
        raise RuntimeError(f"The CI tools do not support {machine}")
    return values[machine]


def _tools_dir() -> Path:
    directory = Path("/tmp/firepit-reactorcide-tools")
    directory.mkdir(parents=True, exist_ok=True)
    return directory


def _download(url: str, destination: Path) -> None:
    log_stdout(f"Download {url}")
    request = urllib.request.Request(url, headers={"User-Agent": "firepit-ci"})
    with urllib.request.urlopen(request, timeout=180) as response:
        with destination.open("wb") as output:
            shutil.copyfileobj(response, output)


def _extract_tar(archive: Path, destination: Path) -> None:
    destination.mkdir(parents=True, exist_ok=True)
    with tarfile.open(archive) as package:
        package.extractall(destination, filter="data")


def _go_environment() -> dict[str, str]:
    home = Path("/tmp/firepit-reactorcide-home")
    directories = {
        "HOME": home,
        "GOPATH": home / "go",
        "GOCACHE": home / ".cache" / "go-build",
        "GOMODCACHE": home / ".cache" / "go-mod",
    }
    for directory in directories.values():
        directory.mkdir(parents=True, exist_ok=True)
    return {name: str(value) for name, value in directories.items()}


def _ensure_go() -> tuple[Path, dict[str, str]]:
    environment = _go_environment()
    existing = shutil.which("go")
    if existing:
        result = subprocess.run(
            (existing, "version"),
            check=False,
            shell=False,
            text=True,
            capture_output=True,
        )
        if f"go{GO_VERSION}" in result.stdout:
            return Path(existing), environment

    go_root = _tools_dir() / f"go-{GO_VERSION}"
    binary = go_root / "bin" / "go"
    if not binary.exists():
        go_arch, _ = _architecture()
        archive = _tools_dir() / f"go-{GO_VERSION}.tar.gz"
        _download(
            f"https://go.dev/dl/go{GO_VERSION}.linux-{go_arch}.tar.gz",
            archive,
        )
        staging = _tools_dir() / f"go-{GO_VERSION}-extract"
        shutil.rmtree(staging, ignore_errors=True)
        _extract_tar(archive, staging)
        shutil.rmtree(go_root, ignore_errors=True)
        shutil.move(str(staging / "go"), go_root)
        shutil.rmtree(staging, ignore_errors=True)
        archive.unlink(missing_ok=True)
    environment["PATH"] = f"{binary.parent}:{os.environ.get('PATH', '')}"
    return binary, environment


def _ensure_node() -> tuple[Path, dict[str, str]]:
    _, node_arch = _architecture()
    node_root = _tools_dir() / f"node-{NODE_VERSION}"
    binary = node_root / "bin" / "node"
    if not binary.exists():
        archive = _tools_dir() / f"node-{NODE_VERSION}.tar.xz"
        _download(
            "https://nodejs.org/dist/"
            f"v{NODE_VERSION}/node-v{NODE_VERSION}-linux-{node_arch}.tar.xz",
            archive,
        )
        staging = _tools_dir() / f"node-{NODE_VERSION}-extract"
        shutil.rmtree(staging, ignore_errors=True)
        _extract_tar(archive, staging)
        extracted = staging / f"node-v{NODE_VERSION}-linux-{node_arch}"
        shutil.rmtree(node_root, ignore_errors=True)
        shutil.move(str(extracted), node_root)
        shutil.rmtree(staging, ignore_errors=True)
        archive.unlink(missing_ok=True)
    environment = {
        "HOME": "/tmp/firepit-reactorcide-home",
        "PATH": f"{binary.parent}:{os.environ.get('PATH', '')}",
    }
    Path(environment["HOME"]).mkdir(parents=True, exist_ok=True)
    return binary, environment


def _validate_conventional_commits(root: Path) -> None:
    diff_base = os.environ.get("REACTORCIDE_DIFF_BASE", "").strip()
    if not diff_base:
        result = _run(
            ("git", "merge-base", "HEAD", "origin/main"),
            cwd=root,
            capture=True,
        )
        diff_base = result.stdout.strip()
    result = _run(
        ("git", "log", f"{diff_base}..HEAD", "--pretty=format:%H%x00%s"),
        cwd=root,
        capture=True,
    )
    failures = []
    for line in result.stdout.splitlines():
        commit_hash, _, subject = line.partition("\x00")
        if CONVENTIONAL_COMMIT.fullmatch(subject):
            log_stdout(f"OK: {subject}")
        else:
            failures.append(f"{commit_hash[:12]} {subject}")
    if failures:
        raise RuntimeError(
            "Commit subjects must use Conventional Commits. Invalid commits:\n"
            + "\n".join(failures)
        )


def _test_go(root: Path) -> None:
    go, environment = _ensure_go()
    for module in (root / "api", root / "coredb"):
        _run((go, "vet", "./..."), cwd=module, env=environment)
        _run((go, "test", "./..."), cwd=module, env=environment)
    _run(
        (go, "test", "-tags=integration", "./..."),
        cwd=root / "api",
        env=environment,
    )


def _test_web(root: Path) -> None:
    _, environment = _ensure_node()
    web = root / "webapp"
    npm = _tools_dir() / f"node-{NODE_VERSION}" / "bin" / "npm"
    _run((npm, "ci"), cwd=web, env=environment)
    _run((npm, "exec", "eslint", "."), cwd=web, env=environment)
    _run((npm, "test"), cwd=web, env=environment)
    _run((npm, "run", "typecheck"), cwd=web, env=environment)
    _run((npm, "run", "build"), cwd=web, env=environment)


def _semver_tags(root: Path, *, dry_run: bool) -> dict[str, Any]:
    go, environment = _ensure_go()
    tool_dir = _tools_dir() / f"semver-tags-{SEMVER_TAGS_VERSION}"
    binary = tool_dir / "semver-tags"
    if not binary.exists():
        tool_dir.mkdir(parents=True, exist_ok=True)
        _run(
            (
                go,
                "install",
                f"github.com/catalystcommunity/semver-tags@{SEMVER_TAGS_VERSION}",
            ),
            cwd=root,
            env={**environment, "GOBIN": str(tool_dir)},
        )
    args = [str(binary), "run", "--output_json", "--branch", ""]
    if dry_run:
        args.append("--dry_run")
    result = _run(args, cwd=root, env=environment, capture=True)
    for line in reversed((result.stdout + "\n" + result.stderr).splitlines()):
        try:
            value = json.loads(line)
        except json.JSONDecodeError:
            continue
        if isinstance(value, dict):
            return value
    raise RuntimeError("semver-tags did not return JSON output")


def _release_plan(metadata: Mapping[str, Any]) -> dict[str, str] | None:
    published = str(metadata.get("New_release_published", "")).lower()
    if published == "false":
        return None
    if published != "true":
        raise RuntimeError("semver-tags returned an invalid release decision")
    tag = str(metadata.get("New_release_git_tag", ""))
    if not RELEASE_TAG.fullmatch(tag):
        raise RuntimeError(f"semver-tags returned an invalid release tag: {tag}")
    return {
        "tag": tag,
        "version": tag.removeprefix("v"),
        "notes": str(metadata.get("New_release_notes", "")).strip(),
    }


def _repository() -> str:
    repository = _required("REACTORCIDE_REPO")
    if not REPOSITORY.fullmatch(repository):
        raise RuntimeError("REACTORCIDE_REPO must use OWNER/REPOSITORY format")
    return repository


def _source_sha(root: Path) -> str:
    result = _run(("git", "rev-parse", "HEAD"), cwd=root, capture=True)
    value = result.stdout.strip()
    if not re.fullmatch(r"[0-9a-f]{40}", value):
        raise RuntimeError("The release source does not have a valid Git SHA")
    return value


def _release_marker(source_sha: str) -> str:
    return f"{RELEASE_MARKER_PREFIX}{source_sha} -->"


def _github_request(
    method: str,
    path: str,
    *,
    payload: Any | None = None,
) -> Any:
    token = _required("GITHUB_PAT")
    data = None if payload is None else json.dumps(payload).encode("utf-8")
    request = urllib.request.Request(
        f"https://api.github.com{path}",
        data=data,
        method=method,
        headers={
            "Accept": "application/vnd.github+json",
            "Authorization": f"Bearer {token}",
            "Content-Type": "application/json",
            "User-Agent": "firepit-release",
            "X-GitHub-Api-Version": GITHUB_API_VERSION,
        },
    )
    try:
        with urllib.request.urlopen(request, timeout=120) as response:
            body = response.read()
    except urllib.error.HTTPError as error:
        raise RuntimeError(
            f"GitHub API request failed with status {error.code}: {method} {path}"
        ) from error
    return json.loads(body) if body else None


def _find_release(repository: str, tag: str) -> dict[str, Any] | None:
    encoded_repository = urllib.parse.quote(repository, safe="/")
    for page in range(1, 6):
        releases = _github_request(
            "GET",
            f"/repos/{encoded_repository}/releases?per_page=100&page={page}",
        )
        if not isinstance(releases, list):
            raise RuntimeError("GitHub returned an invalid release list")
        for release in releases:
            if release.get("tag_name") == tag:
                return release
        if len(releases) < 100:
            break
    return None


def _authorized_release(root: Path) -> dict[str, Any]:
    tag = _release_tag()
    source_sha = _source_sha(root)
    release = _find_release(_repository(), tag)
    if release is None:
        raise RuntimeError(f"No CI-created draft authorizes {tag}")
    if _release_marker(source_sha) not in str(release.get("body") or ""):
        raise RuntimeError("The GitHub release does not authorize this source commit")
    return release


def _git_push_environment(root: Path) -> dict[str, str]:
    token = _required("GITHUB_PAT")
    repository = _repository()
    result = _run(("git", "remote", "get-url", "origin"), cwd=root, capture=True)
    remote = urllib.parse.urlsplit(result.stdout.strip())
    remote_repository = remote.path.strip("/").removesuffix(".git")
    if (
        remote.scheme != "https"
        or remote.hostname != "github.com"
        or remote.username is not None
        or remote_repository.lower() != repository.lower()
    ):
        raise RuntimeError("The origin remote does not match REACTORCIDE_REPO")

    askpass = _tools_dir() / "firepit-git-askpass.py"
    askpass.write_text(
        "#!/usr/bin/env python3\n"
        "import os, sys\n"
        "prompt = sys.argv[1].lower() if len(sys.argv) > 1 else ''\n"
        "name = 'FIREPIT_GIT_USERNAME' if 'username' in prompt "
        "else 'FIREPIT_GIT_PASSWORD'\n"
        "sys.stdout.write(os.environ[name] + '\\n')\n",
        encoding="utf-8",
    )
    askpass.chmod(0o700)
    environment = _go_environment()
    environment.update(
        {
            "GIT_ASKPASS": str(askpass),
            "GIT_TERMINAL_PROMPT": "0",
            "FIREPIT_GIT_USERNAME": "x-access-token",
            "FIREPIT_GIT_PASSWORD": token,
        }
    )
    return environment


def _create_draft(
    repository: str,
    source_sha: str,
    plan: Mapping[str, str],
) -> dict[str, Any]:
    existing = _find_release(repository, plan["tag"])
    if existing is not None:
        if not existing.get("draft"):
            raise RuntimeError(f"A published release already uses {plan['tag']}")
        if _release_marker(source_sha) not in str(existing.get("body") or ""):
            raise RuntimeError(f"An unrelated draft release uses {plan['tag']}")
        log_stdout(f"Reuse draft release {plan['tag']}")
        return existing
    notes = plan["notes"] or "Released by Reactorcide CI."
    encoded_repository = urllib.parse.quote(repository, safe="/")
    release = _github_request(
        "POST",
        f"/repos/{encoded_repository}/releases",
        payload={
            "tag_name": plan["tag"],
            "target_commitish": source_sha,
            "name": plan["tag"],
            "body": f"{notes}\n\n{_release_marker(source_sha)}",
            "draft": True,
            "prerelease": False,
        },
    )
    if not isinstance(release, dict):
        raise RuntimeError("GitHub returned an invalid release")
    return release


def _update_version_files(root: Path, version: str) -> None:
    chart = root / "helm_chart" / "Chart.yaml"
    content = chart.read_text(encoding="utf-8")
    content, chart_count = re.subn(
        r"(?m)^version: .+$",
        f"version: {version}",
        content,
    )
    content, app_count = re.subn(
        r'(?m)^appVersion: .+$',
        f'appVersion: "{version}"',
        content,
    )
    if chart_count != 1 or app_count != 1:
        raise RuntimeError("Chart.yaml does not contain one version and appVersion")
    chart.write_text(content, encoding="utf-8")
    (root / "version" / "VERSION.txt").write_text(f"{version}\n", encoding="utf-8")


def _tag_release(root: Path) -> None:
    repository = _repository()
    _run(
        ("git", "fetch", "--tags", "--force", f"https://github.com/{repository}.git"),
        cwd=root,
    )
    plan = _release_plan(_semver_tags(root, dry_run=True))
    if plan is None:
        log_stdout("No release is required for this source commit")
        return
    source_sha = _source_sha(root)
    _create_draft(repository, source_sha, plan)
    git_environment = _git_push_environment(root)
    actual = _release_plan(_semver_tags_with_environment(root, git_environment))
    if actual is None or actual["tag"] != plan["tag"]:
        raise RuntimeError("semver-tags did not push the planned release tag")

    _update_version_files(root, plan["version"])
    _run(("git", "config", "user.name", "Catalyst Community automation"), cwd=root)
    _run(
        ("git", "config", "user.email", "automation@catalystcommunity.dev"),
        cwd=root,
    )
    _run(("git", "add", "helm_chart/Chart.yaml", "version/VERSION.txt"), cwd=root)
    changed = subprocess.run(
        ("git", "diff", "--cached", "--quiet"),
        cwd=root,
        shell=False,
    ).returncode
    if changed:
        _run(
            ("git", "commit", "-m", f"ci: bump version to {plan['version']}"),
            cwd=root,
        )
        _run(
            ("git", "push", "origin", "HEAD:main"),
            cwd=root,
            env=git_environment,
        )
    log_stdout(f"Pushed release tag {plan['tag']}")


def _semver_tags_with_environment(
    root: Path,
    environment: Mapping[str, str],
) -> dict[str, Any]:
    _, base_environment = _ensure_go()
    binary = _tools_dir() / f"semver-tags-{SEMVER_TAGS_VERSION}" / "semver-tags"
    if not binary.exists():
        raise RuntimeError("semver-tags is not installed")
    result = _run(
        (binary, "run", "--output_json", "--branch", ""),
        cwd=root,
        env={**base_environment, **environment},
        capture=True,
    )
    for line in reversed((result.stdout + "\n" + result.stderr).splitlines()):
        try:
            value = json.loads(line)
        except json.JSONDecodeError:
            continue
        if isinstance(value, dict):
            return value
    raise RuntimeError("semver-tags did not return JSON output")


def _release_tag() -> str:
    if _required("REACTORCIDE_EVENT_TYPE") != "tag_created":
        raise RuntimeError("A release job requires a tag_created event")
    tag = _required("REACTORCIDE_BRANCH")
    if not RELEASE_TAG.fullmatch(tag):
        raise RuntimeError(f"The release tag is invalid: {tag}")
    return tag


def _prepare_release(root: Path) -> None:
    release = _authorized_release(root)
    if not release.get("draft"):
        raise RuntimeError("The authorized release is not a draft")
    log_stdout(f"Authorized release {_release_tag()}")


def _publish_release(root: Path) -> None:
    release = _authorized_release(root)
    if release.get("draft"):
        repository = urllib.parse.quote(_repository(), safe="/")
        _github_request(
            "PATCH",
            f"/repos/{repository}/releases/{release['id']}",
            payload={"draft": False},
        )
    log_stdout(f"Published release {_release_tag()}")


def _install_buildctl() -> Path:
    go_arch, _ = _architecture()
    directory = _tools_dir() / f"buildctl-{BUILDKIT_VERSION}"
    binary = directory / "buildctl"
    if not binary.exists():
        archive = _tools_dir() / f"buildkit-{BUILDKIT_VERSION}.tar.gz"
        _download(
            "https://github.com/moby/buildkit/releases/download/"
            f"v{BUILDKIT_VERSION}/buildkit-v{BUILDKIT_VERSION}.linux-{go_arch}.tar.gz",
            archive,
        )
        staging = _tools_dir() / f"buildkit-{BUILDKIT_VERSION}-extract"
        shutil.rmtree(staging, ignore_errors=True)
        _extract_tar(archive, staging)
        directory.mkdir(parents=True, exist_ok=True)
        shutil.move(str(staging / "bin" / "buildctl"), binary)
        shutil.rmtree(staging, ignore_errors=True)
        archive.unlink(missing_ok=True)
    binary.chmod(0o755)
    return binary


def _copy_source(root: Path, destination: Path) -> None:
    result = _run(("git", "ls-files", "-z"), cwd=root, capture=True)
    for relative in (value for value in result.stdout.split("\x00") if value):
        source = root / relative
        target = destination / relative
        target.parent.mkdir(parents=True, exist_ok=True)
        if source.is_symlink():
            target.symlink_to(os.readlink(source))
        elif source.is_file():
            shutil.copy2(source, target)


def _build_image(root: Path) -> None:
    tag = _release_tag()
    version = RELEASE_TAG.fullmatch(tag).group("version")  # type: ignore[union-attr]
    component = _required("FIREPIT_IMAGE")
    values = {
        "api": ("api/Dockerfile", "firepit-api"),
        "web": ("webapp/Dockerfile", "firepit-webapp"),
    }
    if component not in values:
        raise RuntimeError(f"FIREPIT_IMAGE is invalid: {component}")
    dockerfile, repository_name = values[component]
    registry = _required("REGISTRY")
    registry_user = _required("REGISTRY_USER")
    registry_password = _required("REGISTRY_PASSWORD")
    buildctl = _install_buildctl()
    if not os.environ.get("BUILDKIT_HOST"):
        raise RuntimeError("The builder capability did not set BUILDKIT_HOST")
    for _ in range(30):
        result = subprocess.run(
            (str(buildctl), "debug", "info"),
            cwd=root,
            shell=False,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )
        if result.returncode == 0:
            break
        time.sleep(1)
    else:
        raise RuntimeError("The BuildKit sidecar did not become ready")

    with tempfile.TemporaryDirectory(prefix="firepit-image-") as temporary:
        build_root = Path(temporary) / "source"
        build_root.mkdir()
        _copy_source(root, build_root)
        _update_version_files(build_root, version)
        docker_config = Path(temporary) / "docker"
        docker_config.mkdir(mode=0o700)
        auth = base64.b64encode(
            f"{registry_user}:{registry_password}".encode("utf-8")
        ).decode("ascii")
        (docker_config / "config.json").write_text(
            json.dumps({"auths": {registry: {"auth": auth}}}),
            encoding="utf-8",
        )
        image = f"{registry}/public/catalystcommunity/{repository_name}"
        _run(
            (
                buildctl,
                "build",
                "--frontend",
                "dockerfile.v0",
                "--local",
                f"context={build_root}",
                "--local",
                f"dockerfile={build_root}",
                "--opt",
                f"filename={dockerfile}",
                "--output",
                f"type=image,name={image}:{version},{image}:latest,push=true",
            ),
            cwd=build_root,
            env={"DOCKER_CONFIG": str(docker_config)},
        )
    log_stdout(f"Published {image}:{version}")


def _install_helm() -> Path:
    go_arch, _ = _architecture()
    directory = _tools_dir() / f"helm-{HELM_VERSION}"
    binary = directory / "helm"
    if not binary.exists():
        archive = _tools_dir() / f"helm-{HELM_VERSION}.tar.gz"
        _download(
            f"https://get.helm.sh/helm-{HELM_VERSION}-linux-{go_arch}.tar.gz",
            archive,
        )
        staging = _tools_dir() / f"helm-{HELM_VERSION}-extract"
        shutil.rmtree(staging, ignore_errors=True)
        _extract_tar(archive, staging)
        directory.mkdir(parents=True, exist_ok=True)
        shutil.move(str(staging / f"linux-{go_arch}" / "helm"), binary)
        shutil.rmtree(staging, ignore_errors=True)
        archive.unlink(missing_ok=True)
    binary.chmod(0o755)
    return binary


def _install_kubectl() -> Path:
    go_arch, _ = _architecture()
    directory = _tools_dir() / f"kubectl-{KUBECTL_VERSION}"
    binary = directory / "kubectl"
    if not binary.exists():
        directory.mkdir(parents=True, exist_ok=True)
        _download(
            "https://dl.k8s.io/release/"
            f"{KUBECTL_VERSION}/bin/linux/{go_arch}/kubectl",
            binary,
        )
    binary.chmod(0o755)
    return binary


def _release_smoke(root: Path) -> None:
    helm = _install_helm()
    _run(
        (sys.executable, "-m", "unittest", "discover", "-s", ".reactorcide/tests"),
        cwd=root,
    )
    _run((helm, "lint", "helm_chart"), cwd=root)
    _run(
        (
            helm,
            "template",
            "firepit",
            "helm_chart",
            "-f",
            "deploy/values-catalystsquad.yaml",
            "--set",
            "image.api.tag=ci-smoke",
            "--set",
            "image.webapp.tag=ci-smoke",
            "--set",
            "linkkeys.pki.apiKey=ci-smoke",
            "--set",
            "linkkeysRp.domainKeyPassphrase=ci-smoke",
        ),
        cwd=root,
        capture=True,
    )


def _deploy(root: Path) -> None:
    tag = _release_tag()
    version = RELEASE_TAG.fullmatch(tag).group("version")  # type: ignore[union-attr]
    namespace = _required("K8S_NAMESPACE")
    helm_release = _required("HELM_RELEASE")
    values_file = root / _required("HELM_VALUES_FILE")
    registry_user = _required("REGISTRY_USER")
    registry_password = _required("REGISTRY_PASSWORD")
    helm = _install_helm()
    kubectl = _install_kubectl()

    with tempfile.TemporaryDirectory(prefix="firepit-deploy-") as temporary:
        temporary_path = Path(temporary)
        kubeconfig = temporary_path / "kubeconfig"
        kubeconfig.write_text(_required("KUBECONFIG_CONTENT"), encoding="utf-8")
        kubeconfig.chmod(0o600)
        environment = {"KUBECONFIG": str(kubeconfig)}

        namespace_yaml = _run(
            (
                kubectl,
                "create",
                "namespace",
                namespace,
                "--dry-run=client",
                "-o",
                "yaml",
            ),
            cwd=root,
            env=environment,
            capture=True,
        ).stdout
        _run(
            (kubectl, "apply", "-f", "-"),
            cwd=root,
            env=environment,
            input_text=namespace_yaml,
        )

        secret_args = (
            kubectl,
            "create",
            "secret",
            "docker-registry",
            "regcred",
            "--namespace",
            namespace,
            "--dry-run=client",
            "--docker-server=containers.catalystsquad.com",
            f"--docker-username={registry_user}",
            f"--docker-password={registry_password}",
            "-o",
            "yaml",
        )
        secret_yaml = _run(
            secret_args,
            cwd=root,
            env=environment,
            capture=True,
            sensitive=(registry_user, registry_password),
        ).stdout
        _run(
            (kubectl, "apply", "-f", "-"),
            cwd=root,
            env=environment,
            input_text=secret_yaml,
        )

        chart = temporary_path / "helm_chart"
        shutil.copytree(root / "helm_chart", chart)
        version_directory = temporary_path / "version"
        version_directory.mkdir()
        (version_directory / "VERSION.txt").write_text("0.0.0\n", encoding="utf-8")
        _update_version_files(temporary_path, version)
        runtime_values = temporary_path / "runtime-values.yaml"
        runtime_values.write_text(
            "image:\n"
            f"  api:\n    tag: {json.dumps(version)}\n"
            f"  webapp:\n    tag: {json.dumps(version)}\n"
            "linkkeys:\n"
            f"  pki:\n    apiKey: {json.dumps(_required('LINKKEYS_PKI_API_KEY'))}\n"
            "linkkeysRp:\n"
            "  domainKeyPassphrase: "
            f"{json.dumps(_required('LINKKEYS_RP_DOMAIN_KEY_PASSPHRASE'))}\n",
            encoding="utf-8",
        )
        runtime_values.chmod(0o600)
        _run(
            (
                helm,
                "upgrade",
                "--install",
                "--create-namespace",
                "--namespace",
                namespace,
                helm_release,
                chart,
                "-f",
                values_file,
                "-f",
                runtime_values,
                "--wait",
                "--timeout",
                "5m",
            ),
            cwd=root,
            env=environment,
        )
    log_stdout(f"Deployed Firepit {version}")


class FirepitJobsPlugin(Plugin):
    """Run one selected Firepit job after runnerlib prepares the source."""

    def __init__(self) -> None:
        super().__init__(name="firepit_jobs", priority=100)

    def supported_phases(self) -> list[PluginPhase]:
        return [PluginPhase.POST_SOURCE_PREP]

    def execute(self, context: PluginContext) -> None:
        root = _repo_root(context)
        config_count = int(os.environ.get("GIT_CONFIG_COUNT", "0"))
        os.environ[f"GIT_CONFIG_KEY_{config_count}"] = "safe.directory"
        os.environ[f"GIT_CONFIG_VALUE_{config_count}"] = str(root)
        os.environ["GIT_CONFIG_COUNT"] = str(config_count + 1)
        job = _required("REACTORCIDE_FIREPIT_JOB")
        actions = {
            "conventional-commits": _validate_conventional_commits,
            "test-go": _test_go,
            "test-web": _test_web,
            "release-smoke": _release_smoke,
            "tag": _tag_release,
            "prepare": _prepare_release,
            "image": _build_image,
            "deploy": _deploy,
            "publish": _publish_release,
        }
        action = actions.get(job)
        if action is None:
            raise RuntimeError(f"Unknown REACTORCIDE_FIREPIT_JOB value: {job}")
        action(root)
