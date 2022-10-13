package cleanup

import (
	"context"

	"github.com/go-logr/logr"
	kyvernov1informers "github.com/kyverno/kyverno/pkg/client/informers/externalversions/kyverno/v1"
	kyvernov1listers "github.com/kyverno/kyverno/pkg/client/listers/kyverno/v1"
	"github.com/kyverno/kyverno/pkg/clients/dclient"
	controllerutils "github.com/kyverno/kyverno/pkg/utils/controller"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
)

type controller struct {
	client dclient.Interface
	// listers
	cpolLister kyvernov1listers.ClusterPolicyLister // not sure that we need listers for this one
	polLister  kyvernov1listers.PolicyLister

	// cpolSynced returns true if the cluster policy shared informer has synced at least once
	cpolSynced cache.InformerSynced
	// polSynced returns true if the policy shared informer has synced at least once
	polSynced cache.InformerSynced

	// queue
	queue workqueue.RateLimitingInterface
}

const (
	MaxRetries = 10
	Workers    = 3
)

func NewController(dclient dclient.Interface, cpolInformer kyvernov1informers.ClusterPolicyInformer, polInformer kyvernov1informers.PolicyInformer) *controller {
	c := &controller{
		client:     dclient,
		cpolLister: cpolInformer.Lister(),
		polLister:  polInformer.Lister(),
		cpolSynced: cpolInformer.Informer().HasSynced,
		polSynced:  polInformer.Informer().HasSynced,
		queue:      workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), ControllerName),
	}

	cpolInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: c.enqueue,
		UpdateFunc: func(_, obj interface{}) {
			c.enqueue(obj)
		},
	})
	polInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: c.enqueue,
		UpdateFunc: func(_, obj interface{}) {
			c.enqueue(obj)
		},
	})
	return c
}

func (c *controller) enqueue(obj interface{}) {
	c.queue.Add(obj)
}

func (c *controller) Run(ctx context.Context, workers int) {
	controllerutils.Run(ctx, ControllerName, logger.V(3), c.queue, Workers, MaxRetries, c.reconcile, c.cpolSynced, c.polSynced)
}

func (c *controller) reconcile(ctx context.Context, logger logr.Logger, key, namespace, name string) error {
	policy, err := c.getPolicy(namespace, name)
	if err != nil {
		logger.Error(err, "unable to get the policy from policy informer")
		return err
	}

	for _, rule := range policy.GetSpec().Rules {
		if rule.HasCleanUp() {
			triggers := generateTriggers(c.client, rule, logger)
			for _, trigger := range triggers {
				cronjob := getCronJobForTriggerResource(rule, trigger)
				role, rolebinding := getRoleAndRoleBinding(rule, trigger)
				_, err = c.client.CreateResource("rbac.authorization.k8s.io/v1", "Role", trigger.GetNamespace(), role, false)
				if err != nil {
					logger.Error(err, "unable to create the resource of kind Role for cleanup rule in policy %s", name)
					return err
				}
				_, err = c.client.CreateResource("rbac.authorization.k8s.io/v1", "RoleBinding", trigger.GetNamespace(), rolebinding, false)
				if err != nil {
					logger.Error(err, "unable to create the resource of kind RoleBinding for cleanup rule in policy %s", name)
					return err
				}
				_, err = c.client.CreateResource("batch/v1", "CronJob", trigger.GetNamespace(), cronjob, false)
				if err != nil {
					logger.Error(err, "unable to create the resource of kind CronJob for cleanup rule in policy %s", name)
					return err
				}
			}
		}
	}
	return nil
}
