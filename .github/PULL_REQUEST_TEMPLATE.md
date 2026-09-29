<!--
Thanks for the PR! Before submitting it:

1. It targets `develop`. Only `hotfix/*` branches target `main`.
2. Set ONE `kind/*` label: it defines which CHANGELOG section it appears in.
3. Add or run the relevant tests (coverage shows up in the summary of the CI run).
4. If it is not finished yet, open it as a Draft.
-->

#### What kind of PR is this?

<!--
Label (one):
kind/feature · kind/bug · kind/breaking · kind/deprecation · kind/deps · kind/chore · kind/docs
-->

#### What does this PR do and why is it needed?

#### Which issue(s) does it resolve?

<!--
"Fixes #123" closes the issue on merge. Do not use "Fixes" in kind/flake PRs.
If there is no issue, write N/A.
-->

#### Impact on the public API

<!-- The `api` CI job checks this with apidiff; this is so the reviewer knows up front. -->

- [ ] Does not change the public API
- [ ] Adds compatible API (new functions, types or fields)
- [ ] **Breaks compatibility** → label `kind/breaking`. The release will require `release:major` and updating `/vN` in go.mod
- [ ] Deprecates API → `// Deprecated:` in the godoc, with the alternative

#### Special notes for the reviewer

#### Does this PR introduce a user-visible change for people who use Ego?

<!--
If NOT: write NONE in the block.
If YES: write the note exactly as it should appear in the CHANGELOG, thinking of whoever uses the library
("Adds the `WithTimeout` option to the client.", not "fix timeout").
If consumers have to do something when upgrading, include "action required": the note will also
appear under "Urgent Upgrade Notes".

The pr-meta check fails if the block is left empty.
-->

```release-note

```

#### AI usage

<!--
YES or NO. If YES, briefly describe how it was used.
Whoever opens the PR is responsible for all the code they submit.
-->
