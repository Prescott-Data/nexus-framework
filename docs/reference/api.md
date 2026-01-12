# API Reference

The Dromos OAuth Framework uses OpenAPI 3.0 specifications to define its contracts.

## Gateway API (Public)
The Gateway provides the stable, public-facing API for agents and services.

- **Spec File:** [`openapi.yaml`](../../openapi.yaml)
- **Status:** v1 Frozen.
- **Client SDK:** [`oauth-sdk`](../../oauth-sdk)

## Broker API (Internal)
The Broker provides the internal API for provider management and token operations.

- **Spec File:** [`dromos-oauth-broker/openapi.yaml`](../../dromos-oauth-broker/openapi.yaml)
- **Status:** Internal / Evolving.
