# Pull requests and reviews

lampi has two forges, and each has its own review process. Internal work
goes through the internal Forgejo (`origin`). External agents watch the
public GitHub repository (`github`) and open their pull requests there.
The two processes meet at `main`, which `just sync-github` keeps equal on
both. TKT-01M3D7YX records why this model was chosen over a push mirror.

## Internal work, on Forgejo

1. Branch from `main`, commit, and push the branch to `origin`.
2. Open the pull request with `tea pulls create`.
3. `.forgejo/workflows/ci.yml` runs vet, gofmt, `go test -race`, and a
   build. It is the gate.
4. Request a model review when the pull request is ready. See
   [terva-review](#terva-review).
5. Merge on Forgejo, then run `just sync-github --yes` to fast-forward
   GitHub `main`.

## External agents, on GitHub

An external agent's pull request is reviewed and merged on GitHub.
`.github/workflows/ci.yml` runs the same gates there, without `-race`.
`terva-review` does not run on GitHub. After a merge, run
`just sync-github --yes` to fast-forward Forgejo `main`.

## Keeping main equal

`just sync-github` fetches both `main` branches and fast-forwards the one
that is behind. Without `--yes`, it prints the commits it would push and
stops. It never force-pushes. Neither `main` is branch-protected, so the
fast-forward lands directly.

If both forges merged something since the last sync, the two `main`
branches have diverged and the recipe stops with both heads. Resolve it
this way:

1. Branch from `origin/main` and merge `github/main` into the branch.
2. Land the branch through a Forgejo pull request.
3. Run `just sync-github --yes`. GitHub `main` is now an ancestor of the
   new Forgejo `main`, so it fast-forwards.

Sync after every merge on either forge. A short gap keeps divergence
rare.

## terva-review

`.forgejo/workflows/terva-review.yml` runs the reviewer from
`terva-sh/terva-action-code-review` on internal pull requests. It is
advisory and does not replace CI. Request a review when a pull request is
ready, and again after substantive fixes. A change to `.tickets/**` alone
does not need a new model run. See [Carry](#carry).

Dispatch a review with the pull request number:

```sh
tea actions workflows dispatch terva-review.yml --ref main \
  --input pr=PR_NUMBER --input request-id=ready-review
```

Reuse a request ID to recover a delivery. Change it to get a fresh review
of the same revision. Before the workflow reaches `main`, dispatch from
the branch that adds it (`--ref BRANCH`).

After the workflow is on `main`, `warricksothr` can also request work in a
pull request comment:

```text
/terva review HEAD_SHA BASE_SHA
/terva follow-up REVIEW_ID Your question.
```

To let another maintainer comment commands, add the login to both lists
in the workflow, through a pull request.

### Reading the result

The `terva-review/code` commit status is the gate, and the job result is
not. A review that ran and published ends in a green job whatever it
found. A red job means that the review did not run or did not reach the
pull request. A finding of medium severity or higher fails the status.
Read low-severity findings too.

Record the reviewed head, the review link, and what you did with each
finding in the ticket. A passing model review is evidence. It is not
permission to merge.

### Carry

A dispatch with `--input carry=true` copies the last review's verdict to a
head whose later commits touch only `.tickets/**`. No model runs.

### Trusted configuration

- The job runs the reviewer's published v0.3.0 image, pinned by digest
  (`sha256:35199d57…`). Nothing is checked out, so no code from a lampi
  pull request runs with review credentials.
- `BOT_TOKEN` and `CPA_API_KEY` are organization secrets.
- The provider, base URL, model, and thinking level come from the
  organization's `TERVA_REVIEW_*` Actions variables.
- To move the pin, open a pull request that changes the digest. Follow
  "Adopting a new pin" in the reviewer's `docs/installation.md`, and
  update this section.
