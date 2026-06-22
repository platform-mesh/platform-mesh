/*
Copyright The Platform Mesh Authors.

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

package validation

//go:generate go run schema/genschema.go
type ContentConfiguration struct {
	Name                string              `json:"name,omitempty" yaml:"name,omitempty" jsonschema:"oneof_required=string"`
	CreationTimestamp   string              `json:"creationTimestamp,omitempty" yaml:"creationTimestamp,omitempty"`
	LuigiConfigFragment LuigiConfigFragment `json:"luigiConfigFragment" yaml:"luigiConfigFragment"`
	Url                 string              `json:"url,omitempty" yaml:"url,omitempty"`
}

type LuigiConfigFragment struct {
	Data LuigiConfigData `json:"data,omitempty" yaml:"data,omitempty" jsonschema:"oneof_required=object"`
}

type ViewGroup struct {
	PreloadSuffix             string                    `json:"preloadSuffix,omitempty" yaml:"preloadSuffix,omitempty"`
	RequiredIFramePermissions RequiredIFramePermissions `json:"requiredIFramePermissions,omitempty" yaml:"requiredIFramePermissions,omitempty"`
}

type RequiredIFramePermissions struct {
	Allow   []string `json:"allow,omitempty" yaml:"allow,omitempty"`
	Sandbox []string `json:"sandbox,omitempty" yaml:"sandbox,omitempty"`
}

type LuigiConfigData struct {
	NodeDefaults    *NodeDefaults    `json:"nodeDefaults,omitempty" yaml:"nodeDefaults,omitempty"`
	Nodes           []Node           `json:"nodes,omitempty" yaml:"nodes,omitempty" jsonschema:"oneof_required=array"`
	Texts           []Text           `json:"texts,omitempty" yaml:"texts,omitempty"`
	TargetAppConfig *TargetAppConfig `json:"targetAppConfig,omitempty" yaml:"targetAppConfig,omitempty"`
	ViewGroup       *ViewGroup       `json:"viewGroup,omitempty" yaml:"viewGroup,omitempty"`
	UserSettings    *UserSettings    `json:"userSettings,omitempty" yaml:"userSettings,omitempty"`
}

type UserSettings struct {
	Groups map[string]UserGroupsSetting `json:"groups,omitempty" yaml:"groups,omitempty"`
}

type UserGroupsSetting struct {
	Label    string             `json:"label,omitempty" yaml:"label,omitempty"`
	Sublabel string             `json:"sublabel,omitempty" yaml:"sublabel,omitempty"`
	Title    string             `json:"title,omitempty" yaml:"title,omitempty"`
	Icon     string             `json:"icon,omitempty" yaml:"icon,omitempty"`
	ViewUrl  string             `json:"viewUrl,omitempty" yaml:"viewUrl,omitempty"`
	Initials string             `json:"initials,omitempty" yaml:"initials,omitempty"`
	Settings map[string]Setting `json:"settings,omitempty" yaml:"settings,omitempty"`
}

type Setting struct {
	Type       string   `json:"type,omitempty" yaml:"type,omitempty"`
	Label      string   `json:"label,omitempty" yaml:"label,omitempty"`
	Style      string   `json:"style,omitempty" yaml:"style,omitempty"`
	IsEditable bool     `json:"isEditable,omitempty" yaml:"isEditable,omitempty"`
	Options    []string `json:"options,omitempty" yaml:"options,omitempty"`
}

type TargetAppConfig struct {
	Version        string         `json:"_version,omitempty" yaml:"_version,omitempty"`
	SapIntegration SapIntegration `json:"sap.integration,omitempty" yaml:"sap.integration,omitempty"`
}

type SapIntegration struct {
	NavMode           string            `json:"navMode,omitempty" yaml:"navMode,omitempty"`
	UrlTemplateId     string            `json:"urlTemplateId,omitempty" yaml:"urlTemplateId,omitempty"`
	UrlTemplateParams UrlTemplateParams `json:"urlTemplateParams,omitempty" yaml:"urlTemplateParams,omitempty"`
}

type UrlTemplateParams struct {
	Query interface{} `json:"query,omitempty" yaml:"query,omitempty"`
}

type NodeDefaults struct {
	EntityType  string `json:"entityType,omitempty" yaml:"entityType,omitempty"`
	IsolateView bool   `json:"isolateView,omitempty" yaml:"isolateView,omitempty"`
}

type Text struct {
	Locale         string            `json:"locale,omitempty" yaml:"locale,omitempty"`
	TextDictionary map[string]string `json:"textDictionary" yaml:"textDictionary"`
}

type Node struct {
	EntityType                string                   `json:"entityType,omitempty" yaml:"entityType,omitempty"`
	PathSegment               string                   `json:"pathSegment,omitempty" yaml:"pathSegment,omitempty"`
	Label                     string                   `json:"label,omitempty" yaml:"label,omitempty"`
	Icon                      string                   `json:"icon,omitempty" yaml:"icon,omitempty"`
	Category                  interface{}              `json:"category,omitempty" yaml:"category,omitempty" jsonschema:"anyof_ref=#/$defs/Category,anyof_type=string"`
	Url                       string                   `json:"url,omitempty" yaml:"url,omitempty"`
	HideFromNav               *bool                    `json:"hideFromNav,omitempty" yaml:"hideFromNav,omitempty"`
	VisibleForFeatureToggles  []string                 `json:"visibleForFeatureToggles,omitempty" yaml:"visibleForFeatureToggles,omitempty"`
	VirtualTree               *bool                    `json:"virtualTree,omitempty" yaml:"virtualTree,omitempty"`
	RequiredIFramePermissions interface{}              `json:"requiredIFramePermissions,omitempty" yaml:"requiredIFramePermissions,omitempty" jsonschema:"anyof_type=object"`
	Compound                  interface{}              `json:"compound,omitempty" yaml:"compound,omitempty" jsonschema:"anyof_type=object"`
	InitialRoute              string                   `json:"initialRoute,omitempty" yaml:"initialRoute,omitempty"`
	LayoutConfig              interface{}              `json:"layoutConfig,omitempty" yaml:"layoutConfig,omitempty" jsonschema:"anyof_type=object"`
	Context                   interface{}              `json:"context,omitempty" yaml:"context,omitempty" jsonschema:"anyof_type=object"`
	Webcomponent              interface{}              `json:"webcomponent,omitempty" yaml:"webcomponent,omitempty" jsonschema:"anyof_ref=#/$defs/Webcomponent,anyof_type=boolean"`
	LoadingIndicator          interface{}              `json:"loadingIndicator,omitempty" yaml:"loadingIndicator,omitempty" jsonschema:"anyof_type=object"`
	DefineEntity              *DefineEntity            `json:"defineEntity,omitempty" yaml:"defineEntity,omitempty"`
	KeepSelectedForChildren   *bool                    `json:"keepSelectedForChildren,omitempty" yaml:"keepSelectedForChildren,omitempty"`
	Children                  []Node                   `json:"children,omitempty" yaml:"children,omitempty"`
	UrlSuffix                 string                   `json:"urlSuffix,omitempty" yaml:"urlSuffix,omitempty"`
	HideSideNav               *bool                    `json:"hideSideNav,omitempty" yaml:"hideSideNav,omitempty"`
	TabNav                    *bool                    `json:"tabNav,omitempty" yaml:"tabNav,omitempty"`
	ShowBreadcrumbs           *bool                    `json:"showBreadcrumbs,omitempty" yaml:"showBreadcrumbs,omitempty"`
	DxpOrder                  float32                  `json:"dxpOrder,omitempty" yaml:"dxpOrder,omitempty"`
	Order                     float32                  `json:"order,omitempty" yaml:"order,omitempty"`
	TestId                    string                   `json:"testId,omitempty" yaml:"testId,omitempty"`
	NavSlot                   string                   `json:"navSlot,omitempty" yaml:"navSlot,omitempty"`
	VisibleForPlugin          *bool                    `json:"visibleForPlugin,omitempty" yaml:"visibleForPlugin,omitempty"`
	IsolateView               *bool                    `json:"isolateView,omitempty" yaml:"isolateView,omitempty"`
	VisibleForContext         string                   `json:"visibleForContext,omitempty" yaml:"visibleForContext,omitempty"`
	VisibleForEntityContext   *VisibleForEntityContext `json:"visibleForEntityContext,omitempty" yaml:"visibleForEntityContext,omitempty"`
	NetworkVisibility         string                   `json:"networkVisibility,omitempty" yaml:"networkVisibility,omitempty"`
	ClientPermissions         *ClientPermissions       `json:"clientPermissions,omitempty" yaml:"clientPermissions,omitempty"`
	NavigationContext         string                   `json:"navigationContext,omitempty" yaml:"navigationContext,omitempty"`
	NavHeader                 *NavHeader               `json:"navHeader,omitempty" yaml:"navHeader,omitempty"`
	TitleResolver             *TitleResolver           `json:"titleResolver,omitempty" yaml:"titleResolver,omitempty"`
	DefineSlot                string                   `json:"defineSlot,omitempty" yaml:"defineSlot,omitempty"`
	IgnoreInDocumentTitle     *bool                    `json:"ignoreInDocumentTitle,omitempty" yaml:"ignoreInDocumentTitle,omitempty"`
	ExternalLink              *ExternalLink            `json:"externalLink,omitempty" yaml:"externalLink,omitempty"`
	Link                      string                   `json:"link,omitempty" yaml:"link,omitempty"`
	ViewGroup                 string                   `json:"viewGroup,omitempty" yaml:"viewGroup,omitempty"`
	ConfigurationMissing      string                   `json:"configurationMissing,omitempty" yaml:"configurationMissing,omitempty"`
	ConfigurationHint         string                   `json:"configurationHint,omitempty" yaml:"configurationHint,omitempty"`
	ConfigurationLink         string                   `json:"configurationLink,omitempty" yaml:"configurationLink,omitempty"`
	Id                        string                   `json:"id,omitempty" yaml:"id,omitempty"`
	HideGlobalSearch          *bool                    `json:"hideGlobalSearch,omitempty" yaml:"hideGlobalSearch,omitempty"`
	HideFromBreadcrumb        *bool                    `json:"hideFromBreadcrumb,omitempty" yaml:"hideFromBreadcrumb,omitempty"`
}

type ExternalLink struct {
	Url        string `json:"url,omitempty" yaml:"url,omitempty"`
	SameWindow bool   `json:"sameWindow,omitempty" yaml:"sameWindow,omitempty"`
}

type TitleResolver struct {
	Request            Request `json:"request,omitempty" yaml:"request,omitempty"`
	TitlePropertyChain string  `json:"titlePropertyChain,omitempty" yaml:"titlePropertyChain,omitempty"`
	PrerenderFallback  bool    `json:"prerenderFallback,omitempty" yaml:"prerenderFallback,omitempty"`
	FallbackTitle      string  `json:"fallbackTitle,omitempty" yaml:"fallbackTitle,omitempty"`
	FallbackIcon       string  `json:"fallbackIcon,omitempty" yaml:"fallbackIcon,omitempty"`
}

type Request struct {
	Method  string            `json:"method,omitempty" yaml:"method,omitempty"`
	Url     string            `json:"url,omitempty" yaml:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty" yaml:"headers,omitempty"`
}

type NavHeader struct {
	UseTitleResolver bool   `json:"useTitleResolver,omitempty" yaml:"useTitleResolver,omitempty"`
	Label            string `json:"label,omitempty" yaml:"label,omitempty"`
	ShowUpLink       bool   `json:"showUpLink,omitempty" yaml:"showUpLink,omitempty"`
	Icon             string `json:"icon,omitempty" yaml:"icon,omitempty"`
	Type             string `json:"type,omitempty" yaml:"type,omitempty"`
}

type ClientPermissions struct {
	UrlParameters UrlParameters `json:"urlParameters,omitempty" yaml:"urlParameters,omitempty"`
}

type UrlParameters struct {
	Url    Url `json:"url,omitempty" yaml:"url,omitempty"`
	Q      Url `json:"q,omitempty" yaml:"q,omitempty"`
	Author Url `json:"author,omitempty" yaml:"author,omitempty"`
}

type Url struct {
	Read  bool `json:"read,omitempty" yaml:"read,omitempty"`
	Write bool `json:"write,omitempty" yaml:"write,omitempty"`
}

type VisibleForEntityContext struct {
	Project   Project    `json:"project,omitempty" yaml:"project,omitempty"`
	Component *Component `json:"component,omitempty" yaml:"component,omitempty"`
}

type Type struct {
	Name string `json:"name,omitempty" yaml:"name,omitempty"`
}

type Component struct {
	Annotations map[string]string `json:"annotations,omitempty" yaml:"annotations,omitempty"`
	Id          string            `json:"id,omitempty" yaml:"id,omitempty"`
	Type        Type              `json:"type,omitempty" yaml:"type,omitempty"`
}

type Project struct {
	Policies []string `json:"policies,omitempty" yaml:"policies,omitempty"`
}

type DefineEntity struct {
	Id                    string          `json:"id,omitempty" yaml:"id,omitempty"`
	UseBack               bool            `json:"useBack,omitempty" yaml:"useBack,omitempty"`
	ContextKey            string          `json:"contextKey,omitempty" yaml:"contextKey,omitempty"`
	DynamicFetchId        string          `json:"dynamicFetchId,omitempty" yaml:"dynamicFetchId,omitempty"`
	Label                 string          `json:"label,omitempty" yaml:"label,omitempty"`
	PluralLabel           string          `json:"pluralLabel,omitempty" yaml:"pluralLabel,omitempty"`
	NotFoundConfig        *NotFoundConfig `json:"notFoundConfig,omitempty" yaml:"notFoundConfig,omitempty"`
	GraphqlEntity         *GraphqlEntity  `json:"graphqlEntity,omitempty" yaml:"graphqlEntity,omitempty"`
	AdditionalContextKeys []string        `json:"additionalContextKeys,omitempty" yaml:"additionalContextKeys,omitempty"`
	Type                  string          `json:"type,omitempty" yaml:"type,omitempty"`
}

type GraphqlEntity struct {
	Group   string `json:"group,omitempty" yaml:"group,omitempty"`
	Kind    string `json:"kind,omitempty" yaml:"kind,omitempty"`
	Query   string `json:"query,omitempty" yaml:"query,omitempty"`
	Version string `json:"version,omitempty" yaml:"version,omitempty"`
}

type NotFoundConfig struct {
	EntityListNavigationContext string `json:"entityListNavigationContext,omitempty" yaml:"entityListNavigationContext,omitempty"`
	SapIllusSVG                 string `json:"sapIllusSVG,omitempty" yaml:"sapIllusSVG,omitempty"`
}

type Webcomponent struct {
	SelfRegistered bool   `json:"selfRegistered,omitempty" yaml:"selfRegistered,omitempty"`
	TagName        string `json:"tagName,omitempty" yaml:"tagName,omitempty"`
	Type           string `json:"type,omitempty" yaml:"type,omitempty"`
}

type Category struct {
	Label       string `json:"label,omitempty" yaml:"label,omitempty"`
	Icon        string `json:"icon,omitempty" yaml:"icon,omitempty"`
	Collapsible bool   `json:"collapsible,omitempty" yaml:"collapsible,omitempty"`
	Id          string `json:"id,omitempty" yaml:"id,omitempty"`
	IsGroup     bool   `json:"isGroup,omitempty" yaml:"isGroup,omitempty"`
	Collapsable bool   `json:"collapsable,omitempty" yaml:"collapsable,omitempty"`
	DxpOrder    int    `json:"dxpOrder,omitempty" yaml:"dxpOrder,omitempty"`
	Order       int    `json:"order,omitempty" yaml:"order,omitempty"`
}
