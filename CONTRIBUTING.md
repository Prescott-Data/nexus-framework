# Contributing to Nexus Framework

First off, thank you for considering contributing to the Nexus Framework! It's people like you that make Nexus Framework such a great tool.

## Code of Conduct

By participating in this project, you agree to abide by the terms of our Code of Conduct.

## How Can I Contribute?

### Reporting Bugs

This section guides you through submitting a bug report for Nexus Framework. Following these guidelines helps maintainers and the community understand your report, reproduce the behavior, and find related reports.

*   **Use a clear and descriptive title** for the issue to identify the problem.
*   **Describe the exact steps which reproduce the problem** in as many details as possible.
*   **Describe the behavior you observed after following the steps** and explain precisely what is the problem with that behavior.
*   **Explain which behavior you expected to see instead and why.**

### Suggesting Enhancements

This section guides you through submitting an enhancement suggestion for Nexus Framework, including completely new features and minor improvements to existing functionality.

*   **Use a clear and descriptive title** for the issue to identify the suggestion.
*   **Provide a step-by-step description of the suggested enhancement** in as many details as possible.
*   **Describe the current behavior and explain which behavior you expected to see instead** and why.
*   **Explain why this enhancement would be useful** to most Nexus Framework users.

### Pull Requests

The process described here has several goals:

- Maintain Nexus Framework's quality
- Fix bugs and add features
- Limit the number of Pull Requests that are opened

Please follow these steps to have your contribution considered by the maintainers:

1.  Follow all instructions in [the template](.github/PULL_REQUEST_TEMPLATE.md).
2.  Follow the [Styleguides](#styleguides).
3.  After you submit your pull request, verify that all [status checks](https://help.github.com/articles/about-status-checks/) are passing.

## Styleguides

### Git Commit Messages

*   Use the present tense ("Add feature" not "Added feature")
*   Use the imperative mood ("Move cursor to..." not "Moves cursor to...")
*   Limit the first line to 72 characters or less
*   Reference issues and pull requests liberally after the first line

### Go Styleguide

All Go code should follow the standard `gofmt` formatting and the idioms described in [Effective Go](https://golang.org/doc/effective_go.html).

### Documentation Styleguide

*   Use [Markdown](https://daringfireball.net/projects/markdown/).
*   Reference specific files and directories using relative paths.

## Versioning

One version, read from the root `VERSION` file, everywhere. The rule is enforced by `make check-version`, which CI runs on every pull request and which the merge requires.

- **Releases are pull requests.** To ship a change, bump `VERSION` on your branch, run `make stamp`, and describe the release under `## <version>` at the top of `docs/CHANGELOG.md`. When the PR merges, the release workflow tags `v<version>` and publishes the GitHub Release and container images.
- **Nobody tags by hand.** A repository ruleset blocks `v*` tags from everyone but the release workflow. Creating a release in the GitHub UI is the way the versions drifted in the past; it is no longer possible for contributors.
- **The check fails the PR when** any stamped file disagrees with `VERSION`, the top changelog heading is not `VERSION`, or a tag exists that is newer than `VERSION`.
- **Tags are never moved or deleted.** If a version is wrong, bump past it.

Stamped surfaces: both OpenAPI specs, `mkdocs.yml`, `nexus-sdk-ts/package.json`, `nexus-sdk-python/pyproject.toml` and `nexus_sdk/__init__.py`. Binaries and images read `VERSION` at build time.

## Questions?

Feel free to open an issue for any questions you might have!
