package cleanup

import (
	"fmt"
	"strings"

	"github.com/go-logr/logr"
	kyvernov1alpha1 "github.com/kyverno/kyverno/api/kyverno/v1alpha1"
	"github.com/kyverno/kyverno/pkg/clients/dclient"
	"github.com/kyverno/kyverno/pkg/config"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/kubernetes/pkg/apis/batch"
	api "k8s.io/kubernetes/pkg/apis/core"
)

func (c *controller) getPolicy(namespace, name string) (kyvernov1alpha1.CleanupPolicyInterface, error) {
	if namespace == "" {
		cpolicy, err := c.cpolLister.Get(name)
		if err != nil {
			return nil, err
		}
		return cpolicy, nil
	} else {
		policy, err := c.polLister.CleanupPolicies(namespace).Get(name)
		if err != nil {
			return nil, err
		}
		return policy, nil
	}
}

func generateTriggers(client dclient.Interface, polspec kyvernov1alpha1.CleanupPolicySpec, log logr.Logger) []*unstructured.Unstructured {
	list := &unstructured.UnstructuredList{}

	kinds := fetchUniqueKinds(polspec)

	for _, kind := range kinds {
		mlist, err := client.ListResource("", kind, "", polspec.MatchResources.Selector)
		if err != nil {
			log.Error(err, "failed to list matched resource")
			continue
		}
		list.Items = append(list.Items, mlist.Items...)
	}
	return convertlist(list.Items)
}

func convertlist(ulists []unstructured.Unstructured) []*unstructured.Unstructured {
	var result []*unstructured.Unstructured
	for _, list := range ulists {
		result = append(result, list.DeepCopy())
	}
	return result
}

func fetchUniqueKinds(polspec kyvernov1alpha1.CleanupPolicySpec) []string {
	var kindlist []string

	kindlist = append(kindlist, polspec.MatchResources.Kinds...)

	for _, all := range polspec.MatchResources.Any {
		kindlist = append(kindlist, all.Kinds...)
	}

	if isMatchResourcesAllValid(polspec) {
		for _, all := range polspec.MatchResources.All {
			kindlist = append(kindlist, all.Kinds...)
		}
	}

	inResult := make(map[string]bool)
	var result []string
	for _, kind := range kindlist {
		if _, ok := inResult[kind]; !ok {
			inResult[kind] = true
			result = append(result, kind)
		}
	}
	return result
}

// check if all slice elements are same
func isMatchResourcesAllValid(polspec kyvernov1alpha1.CleanupPolicySpec) bool {
	var kindlist []string
	for _, all := range polspec.MatchResources.All {
		kindlist = append(kindlist, all.Kinds...)
	}

	if len(kindlist) == 0 {
		return false
	}

	for i := 1; i < len(kindlist); i++ {
		if kindlist[i] != kindlist[0] {
			return false
		}
	}
	return true
}

func getCronJobForTriggerResource(pol kyvernov1alpha1.CleanupPolicyInterface, trigger *unstructured.Unstructured) *batch.CronJob {
	command := fmt.Sprintf("kubectl delete %s %s", strings.ToLower(trigger.GetKind()), trigger.GetName())
	cronjob := &batch.CronJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      trigger.GetName(),
			Namespace: trigger.GetNamespace(),
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: trigger.GetAPIVersion(),
					Kind:       trigger.GetKind(),
					Name:       trigger.GetName(),
					UID:        trigger.GetUID(),
				},
			},
		},
		Spec: batch.CronJobSpec{
			Schedule: pol.GetSchedule(),
			JobTemplate: batch.JobTemplateSpec{
				Spec: batch.JobSpec{
					Template: api.PodTemplateSpec{
						Spec: api.PodSpec{
							ServiceAccountName: config.KyvernoServiceAccountName(),
							Containers: []api.Container{
								{
									Name:  trigger.GetName(),
									Image: "bitnami/kubectl:latest",
									Args: []string{
										"/bin/sh",
										"-c",
										command,
									},
								},
							},
						},
					},
				},
			},
		},
	}
	return cronjob
}
