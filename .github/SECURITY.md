# Security Policy

## Supported versions

Urd follows [semver](https://semver.org/) and fixes are always published as a new **patch**
version (the `hotfix/*` → `main` flow).

| Version                                   | Support                                          |
| ----------------------------------------- | ------------------------------------------------ |
| Latest minor of the current major (`vX.Y.*`) | Security and bug fixes                        |
| Earlier minors of the same major          | Not supported. Upgrade to the latest minor: it is compatible |
| Previous major (`vX-1`)                   | Security fixes only, for 6 months after the new major |

Upgrading within the same major does not break compatibility (CI verifies it with `apidiff`),
so the way to receive a fix is always to move to the latest version.

## Reporting a vulnerability

**Do not open a public issue or PR**: the details would be visible to everyone before a fix exists.

Report it privately through GitHub, using the
["Report a Vulnerability"](https://github.com/getsyntegrity/urd/security/advisories/new) feature,
or contact [@pablogore](https://github.com/pablogore) directly.

Include, if you can:

- The affected Urd version (`go list -m github.com/getsyntegrity/urd`)
- A description of the problem and its impact
- Steps or code to reproduce it
- Services that might be exposed

## What happens next

| Stage                          | Target time                  |
| ------------------------------ | ---------------------------- |
| Acknowledgement                | 2 business days              |
| Severity assessment            | 5 business days              |
| Fix published (critical/high)  | As soon as possible, via hotfix |
| Fix published (medium/low)     | In the next release          |

Once the fix is published, the teams that consume Urd are told which version to upgrade to. The
release note marks it as `action required`, so it appears under **Urgent Upgrade Notes** in the
[CHANGELOG](../CHANGELOG/README.md).

## Vulnerable dependencies

- `govulncheck` runs on every PR (it blocks if the code calls something vulnerable) and every night in strict mode.
- Dependabot opens weekly update PRs against `develop`.
