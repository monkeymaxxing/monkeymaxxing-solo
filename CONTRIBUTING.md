# Contributing to Monkeymaxxing Solo

Bug reports, ideas, and pull requests are welcome. `CONTRIBUTING.md` explains
how bug reports, ideas, and pull requests become changes to Monkeymaxxing Solo.

## Reporting a bug or asking for a feature

Open an issue on GitHub. For a bug, include:

- `solo_version` and `solo_commit` from your systems repository's
  `monkeymaxxing_solo_checklist.yml`;
- the setup step or operating procedure where the problem happened;
- the coding agent you use (such as Claude Code, Codex, or Gemini);
- what you expected, and what happened, with the output of the command that
  failed. Remove secrets, keys, and addresses you do not want to publish.

For a feature, start with the problem: what you are trying to do, and how you
do it today. [The design](docs/design.md) gives the reason for each of Solo's
decisions, and a proposal is compared with those reasons.

## Reporting a security problem

Do not open an issue for a security problem: everyone can read issues. Solo's
instructions and playbooks run as root on your server. Report the problem
privately with **Report a vulnerability** on the repository's **Security** tab
on GitHub.

## A pull request is a proposal

Issues and pull requests are welcome. We read a pull request as a proposal.
Maintainers write every change that lands, using their own tools. We do not
merge outside code. When we use a proposal, the commit message credits its
proposer as `Proposed by @name in #N`. We close the pull request with a link
to that commit.

## Where the rules for changes live

| Document | What the document gives you |
|---|---|
| [`AGENTS.md`](AGENTS.md) | Where to start, for people and coding agents alike |
| [The design](docs/design.md) | The conventions every file follows, and the reason for each decision |
| [Acceptance contracts](docs/acceptance.md) | The outcomes every change must keep |
| [Testing guide](docs/testing.md) | Which tests a change needs, and how to run them |

## License

Monkeymaxxing Solo is licensed under the [Apache License 2.0](LICENSE).
Submissions in an issue or a pull request are offered under the Apache
License 2.0, as section 5 of the license says.
