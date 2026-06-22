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

package server

var ERROR_INVALID_JSON_CONTENT = `{
	"contentType": "json",
	"contentConfiguration":"{\"luigiConfigFragment2\": {\"data\": {\"nodeDefaults\": {\"entityType\": \"global\",\"isolateView\": true},\"nodes\": [{\"entityType\": \"global\",\"icon\": \"home\",\"label\": \"Overview\",\"pathSegment\": \"home\"}],\"texts\": [{\"locale\": \"de\",\"textDictionary\": {\"hello\": \"Hallo\"}}]}},\"name\": \"overview\"}"}"
	}`

var OK_VALID_JSON_CONTENT = `{
            "contentType": "json",
			"contentConfiguration":"{\"luigiConfigFragment\": {\"data\": {\"nodeDefaults\": {\"entityType\": \"global\",\"isolateView\": true},\"nodes\": [{\"entityType\": \"global\",\"icon\": \"home\",\"label\": \"Overview\",\"pathSegment\": \"home\"}],\"texts\": [{\"locale\": \"de\",\"textDictionary\": {\"hello\": \"Hallo\"}}]}},\"name\": \"overview\"}"}"
}`

var OK_VALID_YAML_CONTENT = `{
            "contentType": "yaml",
            "contentConfiguration": "luigiConfigFragment:\n  data:\n    nodes:\n    - dxpOrder: 6\n      entityType: global\n      hideSideNav: true\n      icon: business-one\n      label: '{{catalog}}'\n      order: 6\n      pathSegment: catalog\n      showBreadcrumbs: false\n      tabNav: true\n      urlSuffix: /#/global-catalog\n      visibleForFeatureToggles:\n      - '!global-catalog'\n    - dxpOrder: 6\n      entityType: global\n      hideSideNav: true\n      icon: business-one\n      label: '{{catalog}}'\n      order: 6\n      pathSegment: catalog\n      showBreadcrumbs: false\n      tabNav: true\n      urlSuffix: /#/new-global-catalog\n      visibleForFeatureToggles:\n      - global-catalog\n    - children:\n      - context:\n          extClassName: :extClassName\n        hideFromNav: true\n        pathSegment: :extClassName\n        urlSuffix: /#/extensions/:extClassName\n      entityType: global\n      hideFromNav: true\n      label: '{{extensions}}'\n      pathSegment: extensions\n    targetAppConfig:\n      _version: 1.13.0\n      sap.integration:\n        navMode: inplace\n        urlTemplateId: urltemplate.url\n        urlTemplateParams:\n          query: {}\n    texts:\n    - locale: \"\"\n      textDictionary:\n        catalog: Catalog\n        extensions: Extensions\n    - locale: en\n      textDictionary:\n        catalog: Catalog\n        extensions: Extensions\n    - locale: de\n      textDictionary:\n        catalog: Katalog\n        extensions: Erweiterungen\n    viewGroup:\n      preloadSuffix: /#/preload\nname: extension-manager\n"
        }`

var ERROR_INVALID_JSON_CONTENT_WRONG_TYPE = `{
            "contentType": "json",
            "contentConfiguration": "luigiConfigFragment:\n  data:\n    nodes:\n    - dxpOrder: 6\n      entityType: global\n      hideSideNav: true\n      icon: business-one\n      label: '{{catalog}}'\n      order: 6\n      pathSegment: catalog\n      showBreadcrumbs: false\n      tabNav: true\n      urlSuffix: /#/global-catalog\n      visibleForFeatureToggles:\n      - '!global-catalog'\n    - dxpOrder: 6\n      entityType: global\n      hideSideNav: true\n      icon: business-one\n      label: '{{catalog}}'\n      order: 6\n      pathSegment: catalog\n      showBreadcrumbs: false\n      tabNav: true\n      urlSuffix: /#/new-global-catalog\n      visibleForFeatureToggles:\n      - global-catalog\n    - children:\n      - context:\n          extClassName: :extClassName\n        hideFromNav: true\n        pathSegment: :extClassName\n        urlSuffix: /#/extensions/:extClassName\n      entityType: global\n      hideFromNav: true\n      label: '{{extensions}}'\n      pathSegment: extensions\n    targetAppConfig:\n      _version: 1.13.0\n      sap.integration:\n        navMode: inplace\n        urlTemplateId: urltemplate.url\n        urlTemplateParams:\n          query: {}\n    texts:\n    - locale: \"\"\n      textDictionary:\n        catalog: Catalog\n        extensions: Extensions\n    - locale: en\n      textDictionary:\n        catalog: Catalog\n        extensions: Extensions\n    - locale: de\n      textDictionary:\n        catalog: Katalog\n        extensions: Erweiterungen\n    viewGroup:\n      preloadSuffix: /#/preload\nname: extension-manager\n"
        }`

var ERROR_INVALID_JSON_CONTENT2 = `{
	"contentType": "json",
	"contentConfiguration":"{\"luigiConfigFragment2\": {\"data\": {\"nodeDefaults\": {\"entityType\": \"global\",\"isolateView\": true},\"nodes\": [{\"entityType\": \"global\",\"icon\": \"home\",\"label\": \"Overview\",\"pathSegment\": \"home\"}],\"texts\": [{\"locale\": \"de\",\"textDictionary\": {\"hello\": \"Hallo\"}}]}},\"name\": \"overview\"}"}"
	}`

var ERROR_INVALID_JSON_CONTENT_MARSHALLINGVALIDATEDRESPONSE = `{
	"contentType": "json",
	"contentConfiguration":"{\"luigiConfigFragment2\": {\"data\": {\"nodeDefaults\": {\"entityType\": \"global\",\"isolateView\": true},\"nodes\": [{\"entityType\": \"global\",\"icon\": \"home\",\"label\": \"Overview\",\"pathSegment\": \"home\"}],\"texts\": [{\"locale\": \"de\",\"textDictionary\": {\"hello\": \"Hallo\"}}]}},\"name\": \"overview\"}"}"
	}`

var ERROR_INVALID_JSON_CONTENT3 = `not_a_json`
