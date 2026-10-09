// policy-spike renders an experimental policy plan. It never deploys resources
// or treats a supplied image record as verified registry provenance.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/kaimahi-agents/kaimahi/internal/kmx/agentsuite"
	"github.com/kaimahi-agents/kaimahi/internal/kmx/governance"
	"github.com/kaimahi-agents/kaimahi/internal/kmx/imagelift"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "policy-spike:", err)
		os.Exit(1)
	}
}

func run() error {
	policyPath := flag.String("policy", "", "experimental policy JSON")
	gatewayEgress := flag.Bool("gateway-egress", false, "project HTTPS authorities only; does not enforce the whole policy")
	recordPath := flag.String("record", "", "image execution record JSON (not registry-verified)")
	environmentPath := flag.String("environment", "", "explicit lift environment JSON")
	image := flag.String("image", "", "digest-pinned image reference")
	commandJSON := flag.String("command", "", "workload argv as a JSON array; image must contain Python 3")
	flag.Parse()
	if flag.NArg() != 0 || *policyPath == "" {
		return errors.New("-policy is required and positional arguments are unsupported")
	}
	raw, err := read(*policyPath)
	if err != nil {
		return err
	}
	document, err := agentsuite.DecodePolicy(raw)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if *gatewayEgress {
		if *recordPath != "" || *environmentPath != "" || *image != "" || *commandJSON != "" {
			return errors.New("-gateway-egress must not be combined with deployment flags")
		}
		projection, err := governance.ProjectGatewayEgress(document)
		if err != nil {
			return err
		}
		return encoder.Encode(projection)
	}
	if *recordPath == "" || *environmentPath == "" || *image == "" || *commandJSON == "" {
		return errors.New("deployment rendering requires -record, -environment, -image and -command")
	}
	var command []string
	if err := json.Unmarshal([]byte(*commandJSON), &command); err != nil {
		return err
	}
	raw, err = read(*recordPath)
	if err != nil {
		return err
	}
	record, err := agentsuite.DecodeImageDeployment(raw)
	if err != nil {
		return err
	}
	raw, err = read(*environmentPath)
	if err != nil {
		return err
	}
	environment, err := imagelift.DecodeEnvironment(raw)
	if err != nil {
		return err
	}
	if len(environment.Members) != 0 {
		return errors.New("spike accepts one member; whole-suite governance is a follow-up")
	}
	plan, enforcement, err := imagelift.RenderPolicySpike(*image, *record, environment, document, command)
	if err != nil {
		return err
	}
	return encoder.Encode(struct {
		Deployment  imagelift.Plan  `json:"deployment"`
		Enforcement governance.Plan `json:"enforcement"`
	}{plan, enforcement})
}

func read(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, (4<<20)+1))
	if err == nil && len(data) > 4<<20 {
		return nil, errors.New("input exceeds 4 MiB")
	}
	return data, err
}
