# Contributing

1. Fork the repository and create a branch.
2. Run `make lint test` before pushing. Run `make help` for other targets.
3. Open a pull request. CI must pass.
4. Bump the version in `chart/Chart.yaml` (`version` and `appVersion`) and `chart/values.yaml` (`image.tag`) to the same new value. Merging to `main` publishes a release, and the release fails if the version has already been published.

Use British English in code comments and documentation.
