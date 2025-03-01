/*
 Copyright 2021. The KubeVela Authors.

 Licensed under the Apache License, Version 2.0 (the "License");
 you may not use this file except in compliance with the License.
 You may obtain a copy of the License at

     http://www.apache.org/licenses/LICENSE-2.0

 Unless required by applicable law or agreed to in writing, software
 distributed under the License is distributed on an "AS IS" BASIS,
 WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 See the License for the specific language governing permissions and
 limitations under the License.
*/

package query

import (
	"bufio"
	stdctx "context"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	networkv1beta1 "k8s.io/api/networking/v1beta1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/oam-dev/kubevela/apis/core.oam.dev/v1beta1"
	apis "github.com/oam-dev/kubevela/apis/types"
	helmapi "github.com/oam-dev/kubevela/pkg/appfile/helm/flux2apis"
	"github.com/oam-dev/kubevela/pkg/cue/model/value"
	"github.com/oam-dev/kubevela/pkg/multicluster"
	"github.com/oam-dev/kubevela/pkg/utils"
	wfContext "github.com/oam-dev/kubevela/pkg/workflow/context"
	"github.com/oam-dev/kubevela/pkg/workflow/providers"
	"github.com/oam-dev/kubevela/pkg/workflow/types"
)

const (
	// ProviderName is provider name for install.
	ProviderName = "query"
	// HelmReleaseKind is the kind of HelmRelease
	HelmReleaseKind = "HelmRelease"
)

var fluxcdGroupVersion = schema.GroupVersion{Group: "helm.toolkit.fluxcd.io", Version: "v2beta1"}

type provider struct {
	cli client.Client
	cfg *rest.Config
}

// Resource refer to an object with cluster info
type Resource struct {
	Cluster   string                     `json:"cluster"`
	Component string                     `json:"component"`
	Revision  string                     `json:"revision"`
	Object    *unstructured.Unstructured `json:"object"`
}

// Option is the query option
type Option struct {
	Name      string       `json:"name"`
	Namespace string       `json:"namespace"`
	Filter    FilterOption `json:"filter,omitempty"`
}

// FilterOption filter resource created by component
type FilterOption struct {
	Cluster          string   `json:"cluster,omitempty"`
	ClusterNamespace string   `json:"clusterNamespace,omitempty"`
	Components       []string `json:"components,omitempty"`
}

// ServiceEndpoint record the access endpoints of the application services
type ServiceEndpoint struct {
	Endpoint Endpoint               `json:"endpoint"`
	Ref      corev1.ObjectReference `json:"ref"`
}

// String return endpoint URL
func (s *ServiceEndpoint) String() string {
	protocol := strings.ToLower(string(s.Endpoint.Protocol))
	if s.Endpoint.AppProtocol != nil {
		protocol = *s.Endpoint.AppProtocol
	}
	path := s.Endpoint.Path
	if s.Endpoint.Path == "/" {
		path = ""
	}
	if (protocol == "https" && s.Endpoint.Port == 443) || (protocol == "http" && s.Endpoint.Port == 80) {
		return fmt.Sprintf("%s://%s%s", protocol, s.Endpoint.Host, path)
	}
	return fmt.Sprintf("%s://%s:%d%s", protocol, s.Endpoint.Host, s.Endpoint.Port, path)
}

// Endpoint create by ingress or service
type Endpoint struct {
	// The protocol for this endpoint. Supports "TCP", "UDP", and "SCTP".
	// Default is TCP.
	// +default="TCP"
	// +optional
	Protocol corev1.Protocol `json:"protocol,omitempty"`

	// The protocol for this endpoint.
	// Un-prefixed names are reserved for IANA standard service names (as per
	// RFC-6335 and http://www.iana.org/assignments/service-names).
	// +optional
	AppProtocol *string `json:"appProtocol,omitempty"`

	// the host for the endpoint, it could be IP or domain
	Host string `json:"host"`

	// the port for the endpoint
	// Default is 80.
	Port int32 `json:"port"`

	// the path for the endpoint
	Path string `json:"path,omitempty"`
}

// ListResourcesInApp lists CRs created by Application
func (h *provider) ListResourcesInApp(ctx wfContext.Context, v *value.Value, act types.Action) error {
	val, err := v.LookupValue("app")
	if err != nil {
		return err
	}
	opt := Option{}
	if err = val.UnmarshalTo(&opt); err != nil {
		return err
	}
	collector := NewAppCollector(h.cli, opt)
	appResList, err := collector.CollectResourceFromApp()
	if err != nil {
		return v.FillObject(err.Error(), "err")
	}
	return v.FillObject(appResList, "list")
}

func (h *provider) CollectPods(ctx wfContext.Context, v *value.Value, act types.Action) error {
	val, err := v.LookupValue("value")
	if err != nil {
		return err
	}
	cluster, err := v.GetString("cluster")
	if err != nil {
		return err
	}
	obj := new(unstructured.Unstructured)
	if err = val.UnmarshalTo(obj); err != nil {
		return err
	}

	var pods []*unstructured.Unstructured
	var collector PodCollector

	switch obj.GroupVersionKind() {
	case fluxcdGroupVersion.WithKind(HelmReleaseKind):
		collector = helmReleasePodCollector
	default:
		collector = NewPodCollector(obj.GroupVersionKind())
	}

	pods, err = collector(h.cli, obj, cluster)
	if err != nil {
		return v.FillObject(err.Error(), "err")
	}
	return v.FillObject(pods, "list")
}

func (h *provider) SearchEvents(ctx wfContext.Context, v *value.Value, act types.Action) error {
	val, err := v.LookupValue("value")
	if err != nil {
		return err
	}
	cluster, err := v.GetString("cluster")
	if err != nil {
		return err
	}
	obj := new(unstructured.Unstructured)
	if err = val.UnmarshalTo(obj); err != nil {
		return err
	}

	listCtx := multicluster.ContextWithClusterName(stdctx.Background(), cluster)
	fieldSelector := getEventFieldSelector(obj)
	eventList := corev1.EventList{}
	listOpts := []client.ListOption{
		client.InNamespace(obj.GetNamespace()),
		client.MatchingFieldsSelector{
			Selector: fieldSelector,
		},
	}
	if err := h.cli.List(listCtx, &eventList, listOpts...); err != nil {
		return v.FillObject(err.Error(), "err")
	}
	return v.FillObject(eventList.Items, "list")
}

// generatorServiceEndpoints generator service endpoints is available for common component type,
// such as webservice or helm
// it can not support the cloud service component currently
func (h *provider) GeneratorServiceEndpoints(wfctx wfContext.Context, v *value.Value, act types.Action) error {
	ctx := stdctx.Background()
	findResource := func(obj client.Object, name, namespace, cluster string) error {
		obj.SetNamespace(namespace)
		obj.SetName(name)
		gctx, cancel := stdctx.WithTimeout(ctx, time.Second*10)
		defer cancel()
		if err := h.cli.Get(multicluster.ContextWithClusterName(gctx, cluster),
			client.ObjectKeyFromObject(obj), obj); err != nil {
			if kerrors.IsNotFound(err) {
				return nil
			}
			return err
		}
		return nil
	}
	val, err := v.LookupValue("app")
	if err != nil {
		return err
	}
	opt := Option{}
	if err = val.UnmarshalTo(&opt); err != nil {
		return err
	}
	app := new(v1beta1.Application)
	err = findResource(app, opt.Name, opt.Namespace, "")
	if err != nil {
		return fmt.Errorf("query app failure %w", err)
	}
	var serviceEndpoints []ServiceEndpoint
	for _, resource := range app.Status.AppliedResources {
		if !isResourceInTargetCluster(opt.Filter, resource) {
			continue
		}
		switch resource.Kind {
		case "Ingress":
			if resource.GroupVersionKind().Group == networkv1beta1.GroupName && (resource.GroupVersionKind().Version == "v1beta1" || resource.GroupVersionKind().Version == "v1") {
				var ingress networkv1beta1.Ingress
				ingress.SetGroupVersionKind(resource.GroupVersionKind())
				if err := findResource(&ingress, resource.Name, resource.Namespace, resource.Cluster); err != nil {
					klog.Error(err, fmt.Sprintf("find v1 Ingress %s/%s from cluster %s failure", resource.Name, resource.Namespace, resource.Cluster))
					continue
				}
				serviceEndpoints = append(serviceEndpoints, generatorFromIngress(ingress)...)
			} else {
				klog.Warning("not support ingress version", "version", resource.GroupVersionKind())
			}
		case "Service":
			var service corev1.Service
			service.SetGroupVersionKind(resource.GroupVersionKind())
			if err := findResource(&service, resource.Name, resource.Namespace, resource.Cluster); err != nil {
				klog.Error(err, fmt.Sprintf("find v1 Service %s/%s from cluster %s failure", resource.Name, resource.Namespace, resource.Cluster))
				continue
			}
			serviceEndpoints = append(serviceEndpoints, generatorFromService(service)...)
		case helmapi.HelmReleaseGVK.Kind:
			obj := new(unstructured.Unstructured)
			obj.SetNamespace(resource.Namespace)
			obj.SetName(resource.Name)
			hc := NewHelmReleaseCollector(h.cli, obj)
			services, err := hc.CollectServices(ctx, resource.Cluster)
			if err != nil {
				klog.Error(err, "collect service by helm release failure", "helmRelease", resource.Name, "namespace", resource.Namespace, "cluster", resource.Cluster)
			}
			for _, service := range services {
				serviceEndpoints = append(serviceEndpoints, generatorFromService(service)...)
			}

			// only support network/v1beta1
			ingress, err := hc.CollectIngress(ctx, resource.Cluster)
			if err != nil {
				klog.Error(err, "collect ingres by helm release failure", "helmRelease", resource.Name, "namespace", resource.Namespace, "cluster", resource.Cluster)
			}
			for _, ing := range ingress {
				serviceEndpoints = append(serviceEndpoints, generatorFromIngress(ing)...)
			}
		}
	}
	return v.FillObject(serviceEndpoints, "list")
}

var (
	terminatedContainerNotFoundRegex = regexp.MustCompile("previous terminated container .+ in pod .+ not found")
)

func isTerminatedContainerNotFound(err error) bool {
	return err != nil && terminatedContainerNotFoundRegex.MatchString(err.Error())
}

func (h *provider) CollectLogsInPod(ctx wfContext.Context, v *value.Value, act types.Action) error {
	cluster, err := v.GetString("cluster")
	if err != nil {
		return errors.Wrapf(err, "invalid cluster")
	}
	namespace, err := v.GetString("namespace")
	if err != nil {
		return errors.Wrapf(err, "invalid namespace")
	}
	pod, err := v.GetString("pod")
	if err != nil {
		return errors.Wrapf(err, "invalid pod name")
	}
	val, err := v.LookupValue("options")
	if err != nil {
		return errors.Wrapf(err, "invalid log options")
	}
	opts := &corev1.PodLogOptions{}
	if err = val.UnmarshalTo(opts); err != nil {
		return errors.Wrapf(err, "invalid log options content")
	}
	cliCtx := multicluster.ContextWithClusterName(stdctx.Background(), cluster)
	clientSet, err := kubernetes.NewForConfig(h.cfg)
	if err != nil {
		return errors.Wrapf(err, "failed to create kubernetes clientset")
	}
	podInst, err := clientSet.CoreV1().Pods(namespace).Get(cliCtx, pod, v1.GetOptions{})
	if err != nil {
		return errors.Wrapf(err, "failed to get pod")
	}
	req := clientSet.CoreV1().Pods(namespace).GetLogs(pod, opts)
	readCloser, err := req.Stream(cliCtx)
	if err != nil && !isTerminatedContainerNotFound(err) {
		return errors.Wrapf(err, "failed to get stream logs")
	}
	r := bufio.NewReader(readCloser)
	var b strings.Builder
	var readErr error
	if err == nil {
		defer func() {
			_ = readCloser.Close()
		}()
		for {
			s, err := r.ReadString('\n')
			b.WriteString(s)
			if err != nil {
				if !errors.Is(err, io.EOF) {
					readErr = err
				}
				break
			}
		}
	} else {
		readErr = err
	}
	toDate := v1.Now()
	var fromDate v1.Time
	// nolint
	if opts.SinceTime != nil {
		fromDate = *opts.SinceTime
	} else if opts.SinceSeconds != nil {
		fromDate = v1.NewTime(toDate.Add(time.Duration(-(*opts.SinceSeconds) * int64(time.Second))))
	} else {
		fromDate = podInst.CreationTimestamp
	}
	o := map[string]interface{}{
		"logs": b.String(),
		"info": map[string]interface{}{
			"fromDate": fromDate,
			"toDate":   toDate,
		},
	}
	if readErr != nil {
		o["err"] = readErr.Error()
	}
	return v.FillObject(o, "outputs")
}

// Install register handlers to provider discover.
func Install(p providers.Providers, cli client.Client, cfg *rest.Config) {
	prd := &provider{
		cli: cli,
		cfg: cfg,
	}

	p.Register(ProviderName, map[string]providers.Handler{
		"listResourcesInApp":      prd.ListResourcesInApp,
		"collectPods":             prd.CollectPods,
		"searchEvents":            prd.SearchEvents,
		"collectLogsInPod":        prd.CollectLogsInPod,
		"collectServiceEndpoints": prd.GeneratorServiceEndpoints,
	})
}

func generatorFromService(service corev1.Service) []ServiceEndpoint {
	var serviceEndpoints []ServiceEndpoint
	switch service.Spec.Type {
	case corev1.ServiceTypeLoadBalancer:
		for _, port := range service.Spec.Ports {
			for _, ingress := range service.Status.LoadBalancer.Ingress {
				if ingress.Hostname != "" {
					serviceEndpoints = append(serviceEndpoints, ServiceEndpoint{
						Endpoint: Endpoint{
							Protocol: port.Protocol,
							Host:     ingress.Hostname,
							Port:     port.Port,
						},
						Ref: corev1.ObjectReference{
							Kind:            service.Kind,
							Namespace:       service.ObjectMeta.Namespace,
							Name:            service.ObjectMeta.Name,
							UID:             service.UID,
							APIVersion:      service.APIVersion,
							ResourceVersion: service.ResourceVersion,
						},
					})
				}
				if ingress.IP != "" {
					serviceEndpoints = append(serviceEndpoints, ServiceEndpoint{
						Endpoint: Endpoint{
							Protocol: port.Protocol,
							Host:     ingress.IP,
							Port:     port.Port,
						},
						Ref: corev1.ObjectReference{
							Kind:            service.Kind,
							Namespace:       service.ObjectMeta.Namespace,
							Name:            service.ObjectMeta.Name,
							UID:             service.UID,
							APIVersion:      service.APIVersion,
							ResourceVersion: service.ResourceVersion,
						},
					})
				}
			}
		}
	case corev1.ServiceTypeNodePort:
		for _, port := range service.Spec.Ports {
			serviceEndpoints = append(serviceEndpoints, ServiceEndpoint{
				Endpoint: Endpoint{
					Protocol: port.Protocol,
					Port:     port.NodePort,
				},
				Ref: corev1.ObjectReference{
					Kind:            service.Kind,
					Namespace:       service.ObjectMeta.Namespace,
					Name:            service.ObjectMeta.Name,
					UID:             service.UID,
					APIVersion:      service.APIVersion,
					ResourceVersion: service.ResourceVersion,
				},
			})
		}
	case corev1.ServiceTypeClusterIP, corev1.ServiceTypeExternalName:
	}
	return serviceEndpoints
}

func generatorFromIngress(ingress networkv1beta1.Ingress) (serviceEndpoints []ServiceEndpoint) {
	getAppProtocol := func(host string) string {
		if len(ingress.Spec.TLS) > 0 {
			for _, tls := range ingress.Spec.TLS {
				if len(tls.Hosts) > 0 && utils.StringsContain(tls.Hosts, host) {
					return "https"
				}
				if len(tls.Hosts) == 0 {
					return "https"
				}
			}
		}
		return "http"
	}
	// It depends on the Ingress Controller
	getEndpointPort := func(appProtocol string) int {
		if appProtocol == "https" {
			if port, err := strconv.Atoi(ingress.Annotations[apis.AnnoIngressControllerHTTPSPort]); port > 0 && err == nil {
				return port
			}
			return 443
		}
		if port, err := strconv.Atoi(ingress.Annotations[apis.AnnoIngressControllerHTTPPort]); port > 0 && err == nil {
			return port
		}
		return 80
	}
	for _, rule := range ingress.Spec.Rules {
		var appProtocol = getAppProtocol(rule.Host)
		var appPort = getEndpointPort(appProtocol)
		if rule.HTTP != nil {
			for _, path := range rule.HTTP.Paths {
				serviceEndpoints = append(serviceEndpoints, ServiceEndpoint{
					Endpoint: Endpoint{
						Protocol:    corev1.ProtocolTCP,
						AppProtocol: &appProtocol,
						Host:        rule.Host,
						Path:        path.Path,
						Port:        int32(appPort),
					},
					Ref: corev1.ObjectReference{
						Kind:            ingress.Kind,
						Namespace:       ingress.ObjectMeta.Namespace,
						Name:            ingress.ObjectMeta.Name,
						UID:             ingress.UID,
						APIVersion:      ingress.APIVersion,
						ResourceVersion: ingress.ResourceVersion,
					},
				})
			}
		}
	}
	return serviceEndpoints
}
