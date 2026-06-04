package main

import (
	"github.com/pterm/pterm"

	"gloak/internal/flow"
)

type ResumeCmd struct {
	UUID string `arg:"" name:"uuid" help:"Backup UUID to resume."`
}

func (r *ResumeCmd) Run() error {
	pterm.Info.Println("Starting RESUME process...")
	return flow.RunResume(r.UUID)
}
