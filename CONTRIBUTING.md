# Contributing to tolvyn-go

## Releasing: a published tag is immutable

**Never move a tag. Never force-push a tag. Never delete and recreate one.**
To correct a release, publish the next version.

This is not a convention — it is enforced by Go's infrastructure, and the
enforcement is permanent:

- `proxy.golang.org` **caches a module version by its tag** the first time
  anyone fetches it, and serves that cached copy afterwards.
- `sum.golang.org` records a **cryptographic hash** of the module at that
  version, in an append-only transparency log. `github.com/tolvyn/tolvyn-go
  v0.1.4` is in it:

  ```
  github.com/tolvyn/tolvyn-go v0.1.4 h1:aCe/z10lmmMP0hiYqagjro1MQFKslzETq7Y7wfRCm1g=
  ```

- Every customer who builds against that version has those hashes in their
  `go.sum`.

**So moving `v0.1.4` does not publish new code at `v0.1.4`.** It produces a
checksum mismatch for everybody:

```
verifying github.com/tolvyn/tolvyn-go@v0.1.4: checksum mismatch
SECURITY ERROR
This download does NOT match an earlier download recorded in go.sum.
```

Their build stops. The message says SECURITY ERROR, because from Go's point of
view a module that changed under a fixed version is indistinguishable from an
attack — and it cannot be fixed from our side. Not by moving the tag back: the
log entry is append-only, and the proxy may hold either copy.

**It is only reversible before the first fetch, and you cannot know when that
was.** `proxy.golang.org` fetches on demand and caches indefinitely; the window
closes silently the first time any person or CI job anywhere asks for the
version.

### What that means in practice

| situation | what to do |
|---|---|
| a release is wrong | tag the next patch version |
| a tag was pushed early | tag the next version; leave the early one |
| a tag points at the wrong commit | tag the next version |
| the repository history diverged from origin | **never force-push** — re-land the work as a new commit on top of `origin/main` |

### Why this is written down here

On 2026-10-13 this repository's local clone was found to have a history
**disjoint from origin's from the root**, with all five `v0.1.*` tags pointing
at local-only commits that shared their messages with origin's and nothing
else. A `git push --force` would have rewritten every published tag. It did not
happen, and the reason it did not is that somebody checked `git ls-remote`
before pushing rather than trusting the local refs.

The repository was private at the time, so nothing was in `sum.golang.org` yet
and the damage would have been limited. **It went public the same day.** The
window in which that mistake was survivable has closed.

## Before a release

```bash
go build ./...     # includes examples/ — they are real packages, not snippets
go vet ./...
go test ./...
```

`examples/` is deliberately part of the build. Until 2026-10-13 each example
carried `//go:build ignore`, so `go build ./...` passed while all three set a
`ClientOptions` field that does not exist — the three files a customer is most
likely to copy did not compile, and nothing noticed.

## Pushing

```bash
git fetch origin
test -z "$(git log --oneline main..origin/main)" || { echo "behind origin — STOP"; false; }
git push --no-force origin main
```

`git log` exits 0 whether or not it prints, so `git log … && git push` is not a
guard. `--no-force` makes a non-fast-forward fail instead of rewriting history.
