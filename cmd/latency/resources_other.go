//go:build !linux

package main

import "github.com/vmenezesdev/viii-digital-evidence-workbench/internal/spikework"

func recordWorkerResources(*spikework.Output) {}
