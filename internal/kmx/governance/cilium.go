package governance

// This is the only adapter that knows Cilium's CRD vocabulary.
func compileCilium(b ApplicationBinding, agents map[string]resolvedAgent, ids []string) ([]Object, Object) {
	endpoint := func(name string) Object {
		return Object{"matchLabels": Object{"k8s:app": name, "k8s:io.kubernetes.pod.namespace": b.Namespace}}
	}
	ports := func(numbers ...string) []Object {
		out := []Object{}
		for _, n := range numbers {
			out = append(out, Object{"port": n, "protocol": "TCP"})
		}
		return out
	}
	to := func(name string, numbers ...string) Object {
		return Object{"toEndpoints": []Object{endpoint(name)}, "toPorts": []Object{{"ports": ports(numbers...)}}}
	}
	from := func(name string, numbers ...string) Object {
		return Object{"fromEndpoints": []Object{endpoint(name)}, "toPorts": []Object{{"ports": ports(numbers...)}}}
	}
	cnp := func(name string, selector Object, ingress, egress []Object) Object {
		return Object{"apiVersion": "cilium.io/v2", "kind": "CiliumNetworkPolicy", "metadata": metadata(b.Namespace, name),
			"spec": Object{"endpointSelector": selector, "enableDefaultDeny": Object{"ingress": true, "egress": true}, "ingress": ingress, "egress": egress}}
	}
	dns := Object{"toEndpoints": []Object{{"matchLabels": Object{"k8s:io.kubernetes.pod.namespace": "kube-system", "k8s:k8s-app": "kube-dns"}}},
		"toPorts": []Object{{"ports": []Object{{"port": "53", "protocol": "ANY"}}, "rules": Object{"dns": []Object{{"matchPattern": "*"}}}}}}
	out := []Object{cnp("default-deny", Object{}, []Object{}, []Object{}),
		cnp("validator", endpoint("validator"), []Object{}, []Object{{"toEntities": []string{"all"}}})}
	modelIngress := []Object{from("validator", "11434")}
	for _, id := range ids {
		a := agents[id]
		name := a.Binding.Workload
		ingress := []Object{from("validator", "8080")}
		for _, caller := range ids {
			if agents[caller].Peer == id {
				ingress = append(ingress, from("gateway-"+agents[caller].Binding.Workload, "8080"))
			}
		}
		out = append(out, cnp(name, endpoint(name), ingress, []Object{to("gateway-"+name, "3000", "3001")}))
		egress := []Object{dns, to(b.Model.Workload, "11434")}
		if a.Peer != "" {
			egress = append(egress, to(agents[a.Peer].Binding.Workload, "8080"))
		}
		if a.Destination != nil {
			egress = append(egress, Object{"toFQDNs": []Object{{"matchName": a.Destination.Host}}, "toPorts": []Object{{"ports": ports("443")}}})
		}
		out = append(out, cnp("gateway-"+name, endpoint("gateway-"+name), []Object{from(name, "3000", "3001"), from("validator", "3000", "3001")}, egress))
		modelIngress = append(modelIngress, from("gateway-"+name, "11434"))
	}
	out = append(out, cnp(b.Model.Workload, endpoint(b.Model.Workload), modelIngress, []Object{}))
	bootstrap := cnp("model-bootstrap", endpoint(b.Model.Workload), []Object{}, []Object{dns, {"toEntities": []string{"world"}, "toPorts": []Object{{"ports": ports("443")}}}})
	return out, bootstrap
}
