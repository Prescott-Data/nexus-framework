#!/bin/bash
set -a
source .env.dev
set +a
go run ./cmd/nexus-broker
