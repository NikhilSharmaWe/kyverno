package cleanup

import (
	"fmt"
	"strings"

	"github.com/go-logr/logr"
	kyvernov1 "github.com/kyverno/kyverno/api/kyverno/v1"
	"github.com/kyverno/kyverno/pkg/clients/dclient"
	"github.com/kyverno/kyverno/pkg/config"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/kubernetes/pkg/apis/batch"
	api "k8s.io/kubernetes/pkg/apis/core"
)

func (c *controller) getPolicy(namespace, name string) (kyvernov1.PolicyInterface, error) {
	if namespace == "" {
		cpolicy, err := c.cpolLister.Get(name)
		if err != nil {
			return nil, err
		}
		return cpolicy, nil
	} else {
		policy, err := c.polLister.Policies(namespace).Get(name)
		if err != nil {
			return nil, err
		}
		return policy, nil
	}
}

func generateTriggers(client dclient.Interface, rule kyvernov1.Rule, log logr.Logger) []*unstructured.Unstructured {
	list := &unstructured.UnstructuredList{}

	kinds := fetchUniqueKinds(rule)

	for _, kind := range kinds {
		mlist, err := client.ListResource("", kind, "", rule.MatchResources.Selector)
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

func fetchUniqueKinds(rule kyvernov1.Rule) []string {
	var kindlist []string

	kindlist = append(kindlist, rule.MatchResources.Kinds...)

	for _, all := range rule.MatchResources.Any {
		kindlist = append(kindlist, all.Kinds...)
	}

	if isMatchResourcesAllValid(rule) {
		for _, all := range rule.MatchResources.All {
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
func isMatchResourcesAllValid(rule kyvernov1.Rule) bool {
	var kindlist []string
	for _, all := range rule.MatchResources.All {
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

func getCronJobForTriggerResource(rule kyvernov1.Rule, trigger *unstructured.Unstructured) *batch.CronJob {
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
			Schedule: rule.CleanUp.Schedule,
			JobTemplate: batch.JobTemplateSpec{
				Spec: batch.JobSpec{
					// Add configuration for the job responsible for deleting the trigger resource
					// Also need to create corresponding Role, RoleBinding and ServiceAccount
					// resources for letting this CronJob to run kubectl command in the cluster.
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

func getRoleAndRoleBinding(rule kyvernov1.Rule, trigger *unstructured.Unstructured) (*rbacv1.Role, *rbacv1.RoleBinding) {
	role := &rbacv1.Role{
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
		Rules: []rbacv1.PolicyRule{
			{
				Verbs:     []string{"delete"},
				APIGroups: []string{""},
				Resources: []string{fmt.Sprint(strings.ToLower(trigger.GetKind()), "s")},
			},
		},
	}

	rolebinding := &rbacv1.RoleBinding{
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
		Subjects: []rbacv1.Subject{
			{
				Kind: "ServiceAccount",
				Name: config.KyvernoServiceAccountName(),
			},
		},
		RoleRef: rbacv1.RoleRef{
			Kind:     "Role",
			Name:     trigger.GetName(),
			APIGroup: "rbac.authorization.k8s.io",
		},
	}

	return role, rolebinding
}
