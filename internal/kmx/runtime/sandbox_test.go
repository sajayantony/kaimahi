package runtime

import "testing"

func TestSelectSandboxAutoChoosesSmallestCompatibleBackend(t *testing.T) {
	for _, tc := range []struct {
		name string
		req  SandboxRequirements
		want SandboxBackend
	}{
		{"javascript", SandboxRequirements{Language: "js"}, SandboxHyperlightJS},
		{"python", SandboxRequirements{Language: "python"}, SandboxUnikraft},
		{"shell", SandboxRequirements{Shell: true}, SandboxUnikraft},
		{"native packages", SandboxRequirements{Language: "javascript", NativePackages: true}, SandboxUnikraft},
		{"container image", SandboxRequirements{Language: "python", ContainerImage: true}, SandboxPod},
		{"device", SandboxRequirements{Language: "javascript", DeviceAccess: true}, SandboxPod},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan, err := SelectSandbox("auto", tc.req)
			if err != nil {
				t.Fatal(err)
			}
			if plan.Spec.Backend != tc.want {
				t.Fatalf("backend=%q, want %q", plan.Spec.Backend, tc.want)
			}
		})
	}
}

func TestSelectSandboxRefusesMissingOrIncompatibleRequirements(t *testing.T) {
	if _, err := SelectSandbox("auto", SandboxRequirements{}); err == nil {
		t.Fatal("accepted an unexplained automatic choice")
	}
	if _, err := SelectSandbox("hyperlight-js", SandboxRequirements{Language: "python"}); err == nil {
		t.Fatal("accepted Python in Hyperlight JS")
	}
	if _, err := SelectSandbox("unikraft", SandboxRequirements{Language: "python", DeviceAccess: true}); err == nil {
		t.Fatal("accepted device access in Unikraft")
	}
}
