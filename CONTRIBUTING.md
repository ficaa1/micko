# Contributing to micko

## Comments

Write every comment about the code as it stands.

A comment says what the code does, why it is built that way, and what breaks
if it changes. A reader who has never seen this project's history gets the
full value from it.

Keep the history in git. The commit message, the pull request and the issue
hold it already, and they stay accurate as the code moves on. Names for past
events — a review round, a bug report, a test session, a note file, an
earlier behaviour — belong there.

Two comments for the same rule:

    // subtreeAllSkipped reports whether r and every descendant is skipped.
    // Only such a subtree is safe to hide: nothing inside it ran.

    // Hiding skipped branches used to drop the running pod with it, which
    // the hands-on notes flagged in the third round.

The first still reads correctly in a year. The second sends the reader to
another document to learn a rule the code already states.

This covers package docs, function docs, inline notes and test docs alike. A
test doc says what the test pins, not which report produced it.
