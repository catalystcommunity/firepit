# Firepit Reactorcide configuration

Reactorcide uses the files in this directory as trusted CI control code. A pull
request cannot use a new plugin or a new workflow from its own branch. The
first pull request after this configuration reaches `main` uses these files.

The release has two workflows:

1. A merge to `main` creates a draft GitHub release and pushes the next SemVer
   tag.
2. The tag workflow verifies the draft. It builds the API and web images in
   parallel. It deploys both images. It publishes the GitHub release after the
   deployment succeeds.

The release tag is the image version. The tag job also updates
`version/VERSION.txt` and `helm_chart/Chart.yaml` on `main`. The image jobs use
the tag value in their isolated build contexts. This prevents a race with the
version update commit.

## Secret grants

The tracked grant list is the complete Firepit grant policy. It uses exact
secret paths and exact workflow node names. Pull request jobs have no grants.

Apply and prune the list with an authenticated Reactorcide CLI:

```sh
reactorcide secret-grants apply \
  --file .reactorcide/secret-grants.yaml \
  --prune
```

Use `--dry-run` before the apply command when you change this file.

The live project must use `main` as its target branch. It must allow these
events:

- `pull_request_opened`
- `pull_request_updated`
- `pull_request_merged`
- `tag_created`

The live project must use a workflow-capable evaluator image. The current
production value is
`10.16.0.1:5000/public/reactorcide/runnerbase:v0.8.11`.

## Local validation

Use the Reactorcide `runnerlib` source on `PYTHONPATH` to run the plugin tests:

```sh
PYTHONDONTWRITEBYTECODE=1 \
PYTHONPATH="$HOME/repos/catalystcommunity/reactorcide/runnerlib" \
python3 -m unittest discover -s .reactorcide/tests
```
