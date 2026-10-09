package imagelift

import (
	"errors"

	"github.com/kaimahi-agents/kaimahi/agentsuite/policy"
	"github.com/kaimahi-agents/kaimahi/internal/kmx/agentsuite"
	"github.com/kaimahi-agents/kaimahi/internal/kmx/governance"
)

// RenderPolicySpike is opt-in and separate from the installed lift path.
// A trusted administrator must install the content-addressed seccomp profile
// and label eligible nodes. A missing profile fails container creation.
func RenderPolicySpike(image string, record agentsuite.ImageDeployment, env Environment, document policy.Document, command []string) (Plan, governance.Plan, error) {
	if document.Agent != record.Agent || env.Platform.String() != "linux/amd64" || len(command) == 0 || command[0] == "" {
		return Plan{}, governance.Plan{}, errors.New("policy subject, linux/amd64 image identity and explicit workload command are required")
	}
	if err := agentsuite.ValidateImageReference(image); err != nil {
		return Plan{}, governance.Plan{}, err
	}
	enforcement, err := governance.Compile(document, governance.Backend)
	if err != nil {
		return Plan{}, governance.Plan{}, err
	}
	plan, err := Render(image, record, env)
	if err != nil {
		return Plan{}, governance.Plan{}, err
	}
	deployment := plan.Objects[0]
	pod := deployment["spec"].(Object)["template"].(Object)["spec"].(Object)
	delete(pod, "volumes")
	container := pod["containers"].([]any)[0].(Object)
	delete(container, "volumeMounts")
	container["command"] = append([]string{"python", "-B", "-c", enforcement.Launcher}, command...)
	pod["securityContext"].(Object)["runAsUser"] = 65532
	pod["securityContext"].(Object)["runAsGroup"] = 65532
	pod["securityContext"].(Object)["seccompProfile"] = Object{"type": "Localhost", "localhostProfile": enforcement.ProfilePath}
	pod["nodeSelector"].(Object)[enforcement.NodeLabel] = "installed"
	pod["hostNetwork"] = false
	pod["hostPID"] = false
	pod["hostIPC"] = false
	// Bind the base lift identity, policy, compiler output and changed objects.
	plan.Digest, err = governance.Digest(struct {
		Base        Plan
		Enforcement governance.Plan
	}{plan, enforcement})
	if err != nil {
		return Plan{}, governance.Plan{}, err
	}
	for _, object := range plan.Objects {
		annotations := object["metadata"].(Object)["annotations"].(Object)
		annotations[PlanAnnotation] = plan.Digest
		annotations["agentsuite.dev/policy-digest"] = enforcement.PolicyDigest
		annotations["agentsuite.dev/profile-digest"] = enforcement.ProfileDigest
	}
	return plan, enforcement, nil
}
