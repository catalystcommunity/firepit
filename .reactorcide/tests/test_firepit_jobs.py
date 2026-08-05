"""Tests for the Firepit runnerlib plugin and release policy."""

from __future__ import annotations

import importlib.util
import os
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path
from unittest import mock

import yaml

from src.plugins import PluginManager


ROOT = Path(__file__).resolve().parents[2]
PLUGIN_PATH = ROOT / ".reactorcide" / "plugins" / "plugin_firepit_jobs.py"
SPEC = importlib.util.spec_from_file_location("plugin_firepit_jobs", PLUGIN_PATH)
if SPEC is None or SPEC.loader is None:
    raise RuntimeError("The Firepit runnerlib plugin is not available")
PLUGIN = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = PLUGIN
SPEC.loader.exec_module(PLUGIN)


class ConventionalCommitTests(unittest.TestCase):
    def test_supported_subjects_match(self) -> None:
        for subject in (
            "feat: add category filters",
            "fix(api): limit the category query",
            "ci!: replace the release workflow",
        ):
            with self.subTest(subject=subject):
                self.assertIsNotNone(PLUGIN.CONVENTIONAL_COMMIT.fullmatch(subject))

    def test_unsupported_subject_does_not_match(self) -> None:
        self.assertIsNone(PLUGIN.CONVENTIONAL_COMMIT.fullmatch("Update CI"))


class DispatchTests(unittest.TestCase):
    def test_runnerlib_loads_plugin(self) -> None:
        manager = PluginManager()
        manager.load_plugin_from_file(str(PLUGIN_PATH))
        self.assertIn("firepit_jobs", manager.list_plugins())

    def test_plugin_runs_after_source_preparation(self) -> None:
        plugin = PLUGIN.FirepitJobsPlugin()
        self.assertEqual(
            plugin.supported_phases(),
            [PLUGIN.PluginPhase.POST_SOURCE_PREP],
        )

    def test_dispatch_runs_selected_job(self) -> None:
        context = mock.Mock()
        context.config.code_dir = str(ROOT)
        context.metadata = {}
        with (
            mock.patch.dict(
                os.environ,
                {"REACTORCIDE_FIREPIT_JOB": "test-go"},
                clear=False,
            ),
            mock.patch.object(PLUGIN, "_test_go") as test_go,
        ):
            PLUGIN.FirepitJobsPlugin().execute(context)
        test_go.assert_called_once_with(ROOT)


class CommandTests(unittest.TestCase):
    def test_commands_do_not_use_a_shell(self) -> None:
        completed = mock.Mock(spec=subprocess.CompletedProcess)
        with mock.patch.object(
            PLUGIN.subprocess,
            "run",
            return_value=completed,
        ) as run:
            PLUGIN._run(("example", Path("input")), cwd=ROOT)
        self.assertEqual(run.call_args.args[0], ("example", "input"))
        self.assertFalse(run.call_args.kwargs["shell"])

    def test_default_environment_removes_job_secrets(self) -> None:
        with mock.patch.dict(
            os.environ,
            {"GITHUB_PAT": "token", "REGISTRY_PASSWORD": "password"},
            clear=False,
        ):
            environment = PLUGIN._environment()
        self.assertNotIn("GITHUB_PAT", environment)
        self.assertNotIn("REGISTRY_PASSWORD", environment)

    def test_sensitive_arguments_are_redacted(self) -> None:
        completed = mock.Mock(spec=subprocess.CompletedProcess)
        with (
            mock.patch.object(PLUGIN, "log_stdout") as log_stdout,
            mock.patch.object(
                PLUGIN.subprocess,
                "run",
                return_value=completed,
            ),
        ):
            PLUGIN._run(
                ("example", "secret-value"),
                cwd=ROOT,
                sensitive=("secret-value",),
            )
        log_stdout.assert_called_once_with("+ example [REDACTED]")


class ReleaseTests(unittest.TestCase):
    def test_release_plan_accepts_a_version_tag(self) -> None:
        plan = PLUGIN._release_plan(
            {
                "New_release_published": "true",
                "New_release_git_tag": "v1.2.3",
                "New_release_notes": "feat: categories",
            }
        )
        self.assertEqual(plan["version"], "1.2.3")

    def test_release_plan_rejects_a_non_version_tag(self) -> None:
        with self.assertRaisesRegex(RuntimeError, "invalid release tag"):
            PLUGIN._release_plan(
                {
                    "New_release_published": "true",
                    "New_release_git_tag": "latest",
                }
            )

    def test_version_update_changes_all_version_files(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            (root / "helm_chart").mkdir()
            (root / "version").mkdir()
            chart = root / "helm_chart" / "Chart.yaml"
            version_file = root / "version" / "VERSION.txt"
            chart.write_text(
                "version: 0.3.1\nappVersion: \"0.3.1\"\n",
                encoding="utf-8",
            )
            version_file.write_text("0.3.1\n", encoding="utf-8")
            PLUGIN._update_version_files(root, "1.2.3")
            self.assertIn("version: 1.2.3", chart.read_text(encoding="utf-8"))
            self.assertEqual(version_file.read_text(encoding="utf-8"), "1.2.3\n")

    def test_buildkit_output_quotes_multiple_image_names(self) -> None:
        self.assertEqual(
            PLUGIN._image_output("registry.example/firepit-api", "1.2.3"),
            'type=image,"name=registry.example/firepit-api:1.2.3,'
            'registry.example/firepit-api:latest",push=true',
        )


class ConfigurationTests(unittest.TestCase):
    def test_catalystsquad_trusted_domains(self) -> None:
        values = yaml.safe_load(
            (ROOT / "deploy/values-catalystsquad.yaml").read_text(
                encoding="utf-8"
            )
        )
        self.assertTrue(values["seed"]["enabled"])
        self.assertEqual(
            values["seed"]["trustedDomains"],
            ["todandlorna.com", "catalystlinkkeys.com"],
        )

    def test_release_workflow_orders_publish_after_deploy(self) -> None:
        workflow = yaml.safe_load(
            (ROOT / ".reactorcide/workflows/release.yaml").read_text(
                encoding="utf-8"
            )
        )
        jobs = workflow["jobs"]
        self.assertEqual(
            jobs["deploy-production"]["depends_on"],
            ["api-image", "web-image"],
        )
        self.assertEqual(jobs["publish"]["depends_on"], ["deploy-production"])

    def test_pull_request_jobs_have_no_secret_references(self) -> None:
        workflow = yaml.safe_load(
            (ROOT / ".reactorcide/workflows/pr.yaml").read_text(encoding="utf-8")
        )
        for node in workflow["jobs"].values():
            job_file = ROOT / ".reactorcide/jobs" / node["job_file"]
            self.assertNotIn("${secret:", job_file.read_text(encoding="utf-8"))

    def test_grants_match_only_release_workflow_nodes(self) -> None:
        grants = yaml.safe_load(
            (ROOT / ".reactorcide/secret-grants.yaml").read_text(encoding="utf-8")
        )
        subjects = {
            item["spec"]["subject"]["jobName"]["value"] for item in grants["items"]
        }
        self.assertEqual(
            subjects,
            {
                "tag",
                "prepare",
                "api-image",
                "web-image",
                "deploy-production",
                "publish",
            },
        )
        for item in grants["items"]:
            self.assertEqual(item["spec"]["secret"]["match"], "exact")
            self.assertEqual(
                item["spec"]["subject"]["jobName"]["match"], "exact"
            )


if __name__ == "__main__":
    unittest.main()
