# Contributions are welcome

The project adheres to [Semantic Versioning](https://semver.org)
and [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/).
This repo uses Docker-backed `make` targets for its lint, test, and
protobuf-generation workflows. The only host-side prerequisites are
[Docker](https://docs.docker.com/get-docker/) and `make`.

There are two ways you can become a contributor:

1. Request to become a collaborator, and then you can just open pull requests against the repository without forking it.
2. Follow these steps
    - Fork the repository
    - Create a feature branch from `develop`
    - Submit a [pull request](https://help.github.com/articles/using-pull-requests) against `develop`

## Branches, pull requests and releases

Work is integrated on `develop`; `main` only holds released code. Open your pull
request against `develop`. Only `develop` and `hotfix/*` branches may target `main`,
and merging to `main` publishes a new version automatically.

Every pull request needs:

- Exactly one `kind/*` label (`kind/feature`, `kind/bug`, `kind/breaking`,
  `kind/deprecation`, `kind/deps`, `kind/chore`, `kind/docs`), which decides the
  CHANGELOG section.
- A filled-in `release-note` block in the description: the note as a user of the
  library should read it, or `NONE` when nothing visible changes. Include
  `action required` if consumers must do something when upgrading. The `pr-meta`
  check fails on an empty block (`skip-changelog` and `kind/deps` pull requests are exempt).
- A green `ci-ok` check, the single gate that aggregates lint, tests, nested modules,
  `go mod tidy`, API compatibility and vulnerability checks.

To force a version bump on a release pull request, add `release:major`,
`release:minor` or `release:patch`. Without a label a `develop` release is a minor
and a `hotfix/*` release is a patch. Conventional Commits are still the commit style.

## Test & Linter

Prior to submitting a [pull request](https://help.github.com/articles/using-pull-requests),
please build the CI tooling image once and then run lint and tests:

```bash
make docker-image   # builds ego-ci:latest from Dockerfile.ci (one-time per change)
make docker-ci      # runs docker-lint + docker-test
```

Each target also works on its own:

| Target            | Purpose                                                              |
|-------------------|----------------------------------------------------------------------|
| `docker-image`    | Build the hermetic CI image (`ego-ci:latest`) from `Dockerfile.ci`.  |
| `docker-lint`     | Run `golangci-lint` against the working tree.                        |
| `docker-test`     | Run the root module test suite with coverage.                        |
| `docker-protogen` | Regenerate protobuf code via `buf` and refresh `internal/samplepb`.  |
| `docker-ci`       | Composite of `docker-lint` + `docker-test`.                          |

See [`docs/ci.md`](docs/ci.md) for how the GitHub Actions pipeline works and
[`docs/main-branch-policy.md`](docs/main-branch-policy.md) for the branch rules.

The Docker targets mount the working tree at `/workspace` inside the container
and run as your local UID/GID, so any generated files (protobufs,
`coverage.out`) appear in the repo with normal ownership. Go build and module
caches are kept warm between runs in the named volumes `ego-go-build-cache`
and `ego-go-mod-cache`.

If you have the toolchain installed locally, the convenience targets
`make run-eventsourced`, `make run-durablestate`, `make run-saga`, and
`make proto` are still available and execute directly on the host without
Docker.
