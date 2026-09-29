# Working on micko

Read [CONTRIBUTING.md](CONTRIBUTING.md) first: every comment describes the
code as it stands, with no review rounds, issue IDs, plan sections or
release labels.

## Comments

Keep them to a minimum.

- **Say it in the code first.** A good name needs no comment. Comment only
  what the code cannot say: a reason that is not obvious, a constraint, a
  trap.
- **One line where one line will do.** A test's comment names the
  behaviour it protects in one sentence; the test and its table rows show
  the rest. A helper's comment says what it returns, not how it works.
- **Never tell history.** No comment says what the code used to do, which
  bug it fixed or what it replaced. That belongs in the commit message.

  ```go
  // Wrong: history.
  // Leaving the flag set froze the detail view for the rest of the session:
  // the phase stopped moving and r was the only way to see anything new.

  // Right: the behaviour.
  // A stale or canceled detail reply still ends its fetch.
  ```

## Tests

A test earns its place by catching a regression a reader would care about.
Before adding one, answer three questions. If one has no answer, do not add
the test.

1. What behaviour does it protect, as the user or the calling package sees
   it?
2. What change to the code would make it fail?
3. Which existing test already catches that? If one does, extend it instead.

### Rules

- **One owner per behaviour.** A behaviour has one test that owns it. A new
  variation is a new row in that test's table (`for _, c := range cases` or
  `t.Run`), not a new function.
- **Test through what the app calls.** A package's tests go through the
  entry points its production callers use. For a pane, that is `Update`,
  `SetSize` and the methods the shell draws from (`BodyLines`,
  `PaneTitle`, `Hints`, `PaneStatus`). A test in the same package may read
  unexported fields; it must not need an exported one.
- **No production code for tests.** Do not add an exported function, an
  accessor, an option or a second rendering path that only tests call.
  Before relying on a function in a test, check that the app calls it
  (`go run golang.org/x/tools/cmd/deadcode@latest ./cmd/...` lists what
  it never reaches).
- **Unused code goes with its tests.** When code has no production caller,
  delete it and the tests that pin it. A test of unreachable code pins
  behaviour no user can see, and it reads like a real contract.
- **A test must be able to fail.** Compare against a value the code under
  test did not produce. A test that logs instead of failing, or checks its
  input against itself, protects nothing.
- **Prove it fails.** Before committing a regression test, break the code
  it guards and watch the test go red, then restore the code.
- **Use the app's real setup.** Size panes, pick themes and set options the
  way the app does. A test that passes only at one lucky pane height hides
  the heights where the behaviour breaks; cover a range when the result
  depends on geometry.
- **Golden files show what the user sees.** Render the pane the way the
  shell draws it. Regenerate with `UPDATE_GOLDEN=1 go test ./<package>` and
  read every changed line before committing.

## Before pushing

```sh
gofmt -l .
go vet ./... && go vet -tags integration ./... && go vet -tags e2e ./...
go test ./...
go test -tags integration ./tests/...
go run honnef.co/go/tools/cmd/staticcheck@latest -checks U1000 -tags integration,e2e ./...
```
