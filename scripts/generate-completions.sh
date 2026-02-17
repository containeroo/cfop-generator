#!/usr/bin/env sh
set -eu

mkdir -p completions
go run . completion bash > completions/cfop-generator.bash
go run . completion zsh > completions/cfop-generator.zsh
