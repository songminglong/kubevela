/*
Copyright 2021 The KubeVela Authors.

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

package types

import "github.com/oam-dev/kubevela/pkg/oam"

const (
	// DefaultKubeVelaNS defines the default KubeVela namespace in Kubernetes
	DefaultKubeVelaNS = "vela-system"
	// DefaultKubeVelaReleaseName defines the default name of KubeVela Release
	DefaultKubeVelaReleaseName = "kubevela"
	// DefaultKubeVelaChartName defines the default chart name of KubeVela, this variable MUST align to the chart name of this repo
	DefaultKubeVelaChartName = "vela-core"
	// DefaultKubeVelaVersion defines the default version needed for KubeVela chart
	DefaultKubeVelaVersion = ">0.0.0-0"
	// DefaultEnvName defines the default environment name for Apps created by KubeVela
	DefaultEnvName = "default"
	// DefaultAppNamespace defines the default K8s namespace for Apps created by KubeVela
	DefaultAppNamespace = "default"
	// AutoDetectWorkloadDefinition defines the default workload type for ComponentDefinition which doesn't specify a workload
	AutoDetectWorkloadDefinition = "autodetects.core.oam.dev"
)

const (
	// AnnoDefinitionDescription is the annotation which describe what is the capability used for in a WorkloadDefinition/TraitDefinition Object
	AnnoDefinitionDescription = "definition.oam.dev/description"
	// AnnoDefinitionAppliedWorkloads is the annotation which describe what is the workloads used for in a TraitDefinition Object
	AnnoDefinitionAppliedWorkloads = "definition.oam.dev/appliedWorkloads"
	// LabelDefinition is the label for definition
	LabelDefinition = "definition.oam.dev"
	// LabelDefinitionName is the label for definition name
	LabelDefinitionName = "definition.oam.dev/name"
	// LabelDefinitionDeprecated is the label which describe whether the capability is deprecated
	LabelDefinitionDeprecated = "custom.definition.oam.dev/deprecated"
	// LabelDefinitionHidden is the label which describe whether the capability is hidden by UI
	LabelDefinitionHidden = "custom.definition.oam.dev/ui-hidden"
	// AnnoIngressControllerHTTPSPort define ingress controller listen port for https
	AnnoIngressControllerHTTPSPort = "ingress.controller/https-port"
	// AnnoIngressControllerHTTPPort define ingress controller listen port for http
	AnnoIngressControllerHTTPPort = "ingress.controller/http-port"
)

const (
	// StatusDeployed represents the App was deployed
	StatusDeployed = "Deployed"
	// StatusStaging represents the App was changed locally and it's spec is diff from the deployed one, or not deployed at all
	StatusStaging = "Staging"
)

// Config contains key/value pairs
type Config map[string]string

// EnvMeta stores the namespace for app environment
type EnvMeta struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Current   string `json:"current"`
}

const (
	// TagCommandType used for tag cli category
	TagCommandType = "commandType"

	// TagCommandOrder defines the order
	TagCommandOrder = "commandOrder"

	// TypeStart defines one category
	TypeStart = "Getting Started"

	// TypeApp defines one category
	TypeApp = "Managing Applications"

	// TypeCD defines workflow Management operations
	TypeCD = "Continuous Delivery"

	// TypeExtension defines one category
	TypeExtension = "Managing Extension"

	// TypeSystem defines one category
	TypeSystem = "Others"

	// TypePlugin defines one category used in Kubectl Plugin
	TypePlugin = "Plugin Command"
)

// LabelArg is the argument `label` of a definition
const LabelArg = "label"

// DefaultFilterAnnots are annotations that won't pass to workload or trait
var DefaultFilterAnnots = []string{
	oam.AnnotationAppRollout,
	oam.AnnotationRollingComponent,
	oam.AnnotationInplaceUpgrade,
	oam.AnnotationFilterLabelKeys,
	oam.AnnotationFilterAnnotationKeys,
	oam.AnnotationLastAppliedConfiguration,
}

// Cluster contains base info of cluster
type Cluster struct {
	Name     string
	Type     string
	EndPoint string
	Accepted bool
}
