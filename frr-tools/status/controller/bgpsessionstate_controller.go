// SPDX-License-Identifier:Apache-2.0

package controller

import (
	"context"
	"net/netip"
	"reflect"
	"strings"
	"time"

	frrk8sv1beta1 "github.com/metallb/frr-k8s/api/v1beta1"
	"github.com/metallb/frr-k8s/internal/frr"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
)

const (
	nodeLabel       = "frrk8s.metallb.io/node"
	peerLabel       = "frrk8s.metallb.io/peer"
	vrfLabel        = "frrk8s.metallb.io/vrf"
	noBFDConfigured = "N/A"
)

type BGPPeersFetcher func() (map[string][]*frr.Neighbor, error)
type BFDPeersFetcher func() (map[string][]frr.BFDPeer, error)

// BGPSessionStateReconciler reconciles a BGPSessionState object.
type BGPSessionStateReconciler struct {
	client.Client
	BGPPeersFetcher
	BFDPeersFetcher
	Logger       log.Logger
	NodeName     string
	Namespace    string
	DaemonPod    *corev1.Pod
	ResyncPeriod time.Duration
}

// +kubebuilder:rbac:groups=frrk8s.metallb.io,resources=bgpsessionstates,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=frrk8s.metallb.io,resources=bgpsessionstates/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=frrk8s.metallb.io,resources=frrnodestates,verbs=get;list;watch
// +kubebuilder:rbac:groups=frrk8s.metallb.io,resources=frrnodestates/status,verbs=get
// +kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch

func (r *BGPSessionStateReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	level.Info(r.Logger).Log("controller", "BGPSessionState", "start reconcile", req.NamespacedName.String())
	defer level.Info(r.Logger).Log("controller", "BGPSessionState", "end reconcile", req.NamespacedName.String())

	l := frrk8sv1beta1.BGPSessionStateList{}
	err := r.Client.List(ctx, &l, client.MatchingLabels{nodeLabel: r.NodeName})
	if err != nil {
		return ctrl.Result{}, err
	}

	existing := map[string]map[string]*frrk8sv1beta1.BGPSessionState{} // vrf -> peer -> existing status
	for _, s := range l.Items {
		s := s
		if _, ok := existing[vrfFor(s)]; !ok {
			existing[vrfFor(s)] = map[string]*frrk8sv1beta1.BGPSessionState{}
		}
		if _, ok := existing[vrfFor(s)][peerFor(s)]; !ok {
			existing[vrfFor(s)][peerFor(s)] = &s
			continue
		}
		// we shouldn't reach this point, delete duplicates just in case
		err := r.Client.Delete(ctx, &s)
		if err != nil && !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
	}

	neighbors, err := r.BGPPeersFetcher()
	if err != nil {
		return ctrl.Result{}, err
	}
	neighbors = renameDefaultVRF(neighbors)

	bfds, err := r.BFDPeersFetcher()
	if err != nil {
		return ctrl.Result{}, err
	}
	bfds = renameDefaultVRF(bfds)

	bfdForPeer := map[string]map[string]string{}
	for vrf, bfdPeers := range bfds {
		bfdForPeer[vrf] = map[string]string{}
		for _, bfdPeer := range bfdPeers {
			bfdForPeer[vrf][bfdPeer.Peer] = bfdPeer.Status
		}
	}

	toApply := map[string]map[string]*frrk8sv1beta1.BGPSessionState{}

	for vrf, neighs := range neighbors {
		toApply[vrf] = map[string]*frrk8sv1beta1.BGPSessionState{}
		for _, neigh := range neighs {
			var s, curr *frrk8sv1beta1.BGPSessionState
			if existingForVRF, ok := existing[vrf]; ok {
				curr = existingForVRF[statusFormatFor(neigh.ID)]
			}
			if curr != nil {
				s = curr.DeepCopy()
			}
			if s == nil {
				s = &frrk8sv1beta1.BGPSessionState{
					ObjectMeta: metav1.ObjectMeta{
						GenerateName: r.NodeName + "-",
						Namespace:    r.Namespace,
					},
				}
			}
			s.Labels = map[string]string{
				nodeLabel: r.NodeName,
				peerLabel: statusFormatFor(neigh.ID),
				vrfLabel:  vrf,
			}
			bfdStatus := ""
			if _, ok := bfdForPeer[vrf]; ok {
				bfdStatus = bfdForPeer[vrf][neigh.ID]
			}
			if bfdStatus == "" {
				bfdStatus = noBFDConfigured
			}
			s.Status = frrk8sv1beta1.BGPSessionStateStatus{
				Node:      r.NodeName,
				Peer:      statusFormatFor(neigh.ID),
				VRF:       vrf,
				BGPStatus: neigh.BGPState,
				BFDStatus: bfdStatus,
			}

			delete(existing[vrf], statusFormatFor(neigh.ID))
			if curr != nil && reflect.DeepEqual(s.Labels, curr.Labels) && reflect.DeepEqual(s.Status, curr.Status) {
				continue
			}
			toApply[vrf][neigh.ID] = s
		}
	}

	errs := []error{}
	for _, states := range existing { // delete the existing statuses that belong to non-existing neighbors
		for _, s := range states {
			s := s
			err := r.Client.Delete(ctx, s)
			if err != nil && !apierrors.IsNotFound(err) {
				errs = append(errs, err)
			}
		}
	}

	for _, states := range toApply {
		for _, s := range states {
			s := s
			desiredStatus := s.Status
			_, err := controllerutil.CreateOrPatch(ctx, r.Client, s, func() error {
				err = controllerutil.SetOwnerReference(r.DaemonPod, s, r.Scheme())
				if err != nil {
					return err
				}
				s.Status = desiredStatus
				return nil
			})
			if err != nil {
				errs = append(errs, err)
			}
		}
	}

	if utilerrors.NewAggregate(errs) != nil {
		return ctrl.Result{}, utilerrors.NewAggregate(errs)
	}

	// We use the ResyncPeriod for requeuing the node's FRRNodeState, relying on it being
	// the only non-namespaced resource with the node's name that triggers the reconciliation.
	if req.Name == r.NodeName && req.Namespace == "" {
		return ctrl.Result{RequeueAfter: r.ResyncPeriod}, nil
	}

	return ctrl.Result{}, nil
}

func (r *BGPSessionStateReconciler) SetupWithManager(mgr ctrl.Manager) error {
	p := predicate.NewPredicateFuncs(func(o client.Object) bool {
		return r.filterBGPSessionStateEvent(o) && r.filterFRRNodeStateEvent(o)
	})

	return ctrl.NewControllerManagedBy(mgr).
		For(&frrk8sv1beta1.BGPSessionState{}).
		Watches(&frrk8sv1beta1.FRRNodeState{}, &handler.EnqueueRequestForObject{}).
		WithEventFilter(p).
		Complete(r)
}

func (r *BGPSessionStateReconciler) filterBGPSessionStateEvent(o client.Object) bool {
	s, ok := o.(*frrk8sv1beta1.BGPSessionState)
	if !ok {
		return true
	}

	if s.Labels == nil {
		return false
	}

	if nodeFor(*s) != r.NodeName {
		return false
	}

	return true
}

func (r *BGPSessionStateReconciler) filterFRRNodeStateEvent(o client.Object) bool {
	s, ok := o.(*frrk8sv1beta1.FRRNodeState)
	if !ok {
		return true
	}

	if s.Name != r.NodeName {
		return false
	}

	return true
}

func nodeFor(s frrk8sv1beta1.BGPSessionState) string {
	return s.Labels[nodeLabel]
}

func peerFor(s frrk8sv1beta1.BGPSessionState) string {
	return s.Labels[peerLabel]
}

func vrfFor(s frrk8sv1beta1.BGPSessionState) string {
	return s.Labels[vrfLabel]
}

func statusFormatFor(id string) string {
	addr, err := netip.ParseAddr(id)
	if err != nil { // can happen in the interface case
		return id
	}
	if addr.Is4() {
		return id
	}
	return strings.ReplaceAll(addr.StringExpanded(), ":", "-") // a label value can't contain ":", and must end with an alphanumeric character
}

// Returns a map with the "default" key set to "". We use this since FRR returns "default" when no VRF is configured.
func renameDefaultVRF[T any](m map[string]T) map[string]T {
	res := map[string]T{}
	for k, v := range m {
		res[k] = v
	}
	res[""] = m["default"]
	delete(res, "default")
	return res
}
