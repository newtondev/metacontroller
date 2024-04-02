# Metacontroller End-to-end Tests

This directory contains Metacontroller's E2E tests. These tests exercise Metacontroller features by:

1. Creating a `kind` cluster.
2. Deploying Metacontroller, using its Helm chart.
3. Applying a series of YAML manifests.
4. Testing whether the resulting resources work as expected.

These tests focus on core Metacontroller features - i.e. the Composite controller, the Decoratort controller, the webhooks, etc. They should not take dependencies on external systems.

All Metacontroller features must be exercised by these tests, as well as unit tests.

## Running Tests

Run `make e2e` to run E2E tests.

