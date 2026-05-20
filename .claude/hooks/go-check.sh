#!/bin/bash
gofmt -w . && go vet ./... && go test ./... 2>&1 | tail -20

