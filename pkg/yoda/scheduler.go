package yoda

import (
	"context"
	"fmt"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog"
	"k8s.io/kubernetes/pkg/scheduler/framework"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"

	scv "github.com/NJUPT-ISL/SCV/api/v1"

	"github.com/NJUPT-ISL/Yoda-Scheduler/pkg/yoda/collection"
	"github.com/NJUPT-ISL/Yoda-Scheduler/pkg/yoda/filter"
	"github.com/NJUPT-ISL/Yoda-Scheduler/pkg/yoda/score"
	"github.com/NJUPT-ISL/Yoda-Scheduler/pkg/yoda/sort"
)

const (
	Name = "yoda"
)

var (
	_ framework.QueueSortPlugin  = &Yoda{}
	_ framework.FilterPlugin     = &Yoda{}
	_ framework.PostFilterPlugin = &Yoda{}
	_ framework.ScorePlugin      = &Yoda{}
	_ framework.ScoreExtensions  = &Yoda{}

	scheme = runtime.NewScheme()
)

type Yoda struct {
	handle framework.Handle
	cache  cache.Cache
}

func (y *Yoda) Name() string {
	return Name
}

func New(_ runtime.Object, h framework.Handle) (framework.Plugin, error) {

	if err := scv.AddToScheme(scheme); err != nil {
		klog.Error(err)
		return nil, err
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:             scheme,
		MetricsBindAddress: "",
		LeaderElection:     false,
		Port:               9443,
	})
	if err != nil {
		klog.Error(err)
		return nil, err
	}
	go func() {
		if err = mgr.Start(ctrl.SetupSignalHandler()); err != nil {
			klog.Error(err)
			panic(err)
		}
	}()

	return &Yoda{
		handle: h,
		cache:  mgr.GetCache(),
	}, nil
}

func (y *Yoda) Filter(ctx context.Context, _ *framework.CycleState, pod *v1.Pod, node *framework.NodeInfo) *framework.Status {
	klog.V(3).Infof("filter pod: %v, node: %v", pod.Name, node.Node().Name)

	currentScv := &scv.Scv{}
	err := y.cache.Get(ctx, types.NamespacedName{Name: node.Node().GetName()}, currentScv)
	if err != nil {
		klog.Errorf("Get SCV Error: %v", err)
		return framework.NewStatus(framework.Unschedulable, "Node:"+node.Node().Name+" "+err.Error())
	}
	if ok, number := filter.PodFitsNumber(pod, currentScv); ok {
		isFitsMemory, _ := filter.PodFitsMemory(number, pod, currentScv)
		isFitsClock, _ := filter.PodFitsClock(number, pod, currentScv)
		if isFitsMemory && isFitsClock {
			return framework.NewStatus(framework.Success, "")
		}
	}
	return framework.NewStatus(framework.Unschedulable, "Node:"+node.Node().Name)
}

func (y *Yoda) PostFilter(ctx context.Context, state *framework.CycleState, pod *v1.Pod, _ framework.NodeToStatusMap) (*framework.PostFilterResult, *framework.Status) {
	klog.V(3).Infof("collect info for scheduling pod: %v", pod.Name)
	scvList := scv.ScvList{}
	if err := y.cache.List(ctx, &scvList); err != nil {
		klog.Errorf("Get Scv List Error: %v", err)
		return &framework.PostFilterResult{}, framework.NewStatus(framework.Error, err.Error())
	}
	return &framework.PostFilterResult{}, collection.CollectMaxValues(state, pod, scvList)
}

func (y *Yoda) Less(podInfo1, podInfo2 *framework.QueuedPodInfo) bool {
	return sort.Less(podInfo1, podInfo2)
}

func (y *Yoda) Score(ctx context.Context, state *framework.CycleState, p *v1.Pod, nodeName string) (int64, *framework.Status) {
	// Get Node Info
	nodeInfo, err := y.handle.SnapshotSharedLister().NodeInfos().Get(nodeName)
	if err != nil {
		return 0, framework.NewStatus(framework.Error, fmt.Sprintf("getting node %q from Snapshot: %v", nodeName, err))
	}

	// Get Scv Info
	currentScv := &scv.Scv{}
	err = y.cache.Get(ctx, types.NamespacedName{Name: nodeName}, currentScv)
	if err != nil {
		klog.Errorf("Get SCV Error: %v", err)
		return 0, framework.NewStatus(framework.Error, fmt.Sprintf("Score Node Error: %v", err))
	}

	uNodeScore, err := score.CalculateScore(currentScv, state, p, nodeInfo)
	if err != nil {
		return 0, framework.NewStatus(framework.Error, fmt.Sprintf("Score Node Error: %v", err))
	}
	nodeScore := filter.Uint64ToInt64(uNodeScore)
	return nodeScore, framework.NewStatus(framework.Success, "")
}

func (y *Yoda) NormalizeScore(_ context.Context, _ *framework.CycleState, pod *v1.Pod, scores framework.NodeScoreList) *framework.Status {
	var (
		highest int64 = 0
		lowest        = scores[0].Score
	)

	for _, nodeScore := range scores {
		if nodeScore.Score < lowest {
			lowest = nodeScore.Score
		}
		if nodeScore.Score > highest {
			highest = nodeScore.Score
		}
	}

	if highest == lowest {
		lowest--
	}

	// Set Range to [0-100]
	for i, nodeScore := range scores {
		scores[i].Score = (nodeScore.Score - lowest) * framework.MaxNodeScore / (highest - lowest)
		klog.Infof("Node: %v, Score: %v in Plugin: Mandalorian When scheduling Pod: %v/%v", scores[i].Name, scores[i].Score, pod.GetNamespace(), pod.GetName())
	}
	return nil
}

func (y *Yoda) ScoreExtensions() framework.ScoreExtensions {
	return y
}
