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

package validation_test

func GetValidJSON() string {
	return `{
		"luigiConfigFragment": {
			"data": {
				"nodeDefaults": {
					"entityType": "global",
					"isolateView": true
				},
				"nodes": [
					{
						"entityType": "global",
						"icon": "home",
						"label": "Overview",
						"pathSegment": "home"
					}
				],
				"texts": [
					{
						"locale": "de",
						"textDictionary": {
							"hello": "Hallo"
						}
					}
				]
			}
		},
		"name": "overview"
	}`
}

func GetValidYAML() string {
	return `
name: overview
luigiConfigFragment:
 data:
  nodeDefaults:
    entityType: global
    isolateView: true
  nodes:
  - entityType: global
    pathSegment: home
    label: Overview
    icon: home
  texts:
  - locale: de
    textDictionary:
      hello: Hallo
`
}

func GetValidJSONWithEmptyLocale() string {
	return `{
		"luigiConfigFragment": {
			"data": {
				"nodeDefaults": {
					"entityType": "global",
					"isolateView": true
				},
				"nodes": [
					{
						"entityType": "global",
						"icon": "home",
						"label": "Overview",
						"pathSegment": "home"
					}
				],
				"texts": [
					{
						"locale": "",
						"textDictionary": {
							"hello": "Hello"
						}
					},
					{
						"locale": "de",
						"textDictionary": {
							"hello": "Hallo"
						}
					}
				]
			}
		},
		"name": "overview"
	}`
}

func GetValidIncompatibleYAML() string {
	return `
iAmOptionalCustomFieldThatShouldBeStored: iAmOptionalCustomValue
name: overview
luigiConfigFragment:
 data:
  nodeDefaults:
    entityType: global
    isolateView: true
  nodes:
  - entityType: global
    pathSegment: home
    label: Overview
    icon: home
  texts:
  - textDictionary:
      hello: Hallo
`
}

func GetInvalidTypeYAML() string {
	return `
name: overview
luigiConfigFragment:
  data:
    nodes: "string"
`
}

func GetValidJSONButDifferentName() string {
	return `{
		"luigiConfigFragment": {
			"data": {
				"nodeDefaults": {
					"entityType": "global",
					"isolateView": true
				},
				"nodes": [
					{
						"entityType": "global",
						"icon": "home",
						"label": "Overview",
						"pathSegment": "home"
					}
				],
				"texts": [
					{
						"locale": "de",
						"textDictionary": {
							"hello": "Hallo"
						}
					}
				]
			}
		},
		"name": "overview2"
	}`
}

func GetValidYAMLFixtureButDifferentName() string {
	return `
name: overview2
luigiConfigFragment:
 data:
  nodeDefaults:
    entityType: global
    isolateView: true
  nodes:
  - entityType: global
    pathSegment: home
    label: Overview
    icon: home
  texts:
  - locale: de
    textDictionary:
      hello: Hallo
`
}

func GetluigiConfigFragment() string {
	return ` {
        "name": "accounts",
        "luigiConfigFragment": {
            "data": {
              "nodes": [
                {
                  "pathSegment": "create",
                  "hideFromNav": true,
                  "entityType": "main",
                  "loadingIndicator": {
                    "enabled": false
                  },
                  "keepSelectedForChildren": true,
                  "url": "https://some.url/modal/create",
                  "children": []
                },
                {
                  "pathSegment": "accounts",
                  "label": "Accounts",
                  "entityType": "main",
                  "loadingIndicator": {
                    "enabled": false
                  },
                  "keepSelectedForChildren": true,
                  "url": "https://some.url/accounts",
                  "children": [
                    {
                      "pathSegment": ":accountId",
                      "hideFromNav": true,
                      "keepSelectedForChildren": false,
                      "defineEntity": {
                        "id": "account"
                      },
                      "context": {
                        "accountId": ":accountId"
                      }
                    }
                  ]
                },
                {
                  "pathSegment": "overview",
                  "label": "Overview",
                  "entityType": "main.account",
                  "loadingIndicator": {
                    "enabled": false
                  },
                  "visibleForFeatureToggles": ["oldAccount"],
                  "url": "https://some.url/accounts/:accountId"
                }
              ]
            }
          }
      }`
}

func GetValidYaml_targetAppConfig_viewGroup() string {
	return `{
  "name": "extension-manager",
  "luigiConfigFragment": {
      "data": {
        "targetAppConfig": {
        "_version": "1.13.0",
        "sap.integration": {
          "navMode": "inplace",
          "urlTemplateId": "urltemplate.url",
          "urlTemplateParams": {
            "query": {}
          }
        }
      },
      "viewGroup": {
        "preloadSuffix": "/#/preload"
      },
      "nodes": [
        {
          "entityType": "global",
          "pathSegment": "catalog",
          "label": "{{catalog}}",
          "icon": "business-one",
          "dxpOrder": 6,
          "order": 6,
          "hideSideNav": true,
          "tabNav": true,
          "showBreadcrumbs": false,
          "urlSuffix": "/#/global-catalog",
          "visibleForFeatureToggles": ["!global-catalog"]
        },
        {
          "entityType": "global",
          "pathSegment": "catalog",
          "label": "{{catalog}}",
          "icon": "business-one",
          "dxpOrder": 6,
          "order": 6,
          "hideSideNav": true,
          "tabNav": true,
          "showBreadcrumbs": false,
          "urlSuffix": "/#/new-global-catalog",
          "visibleForFeatureToggles": ["global-catalog"]
        },
        {
          "entityType": "global",
          "pathSegment": "extensions",
          "label": "{{extensions}}",
          "hideFromNav": true,
          "children": [
            {
              "pathSegment": ":extClassName",
              "hideFromNav": true,
              "urlSuffix": "/#/extensions/:extClassName",
              "context": {
                "extClassName": ":extClassName"
              }
            }
          ]
        }
      ],
      "texts": [
        {
          "locale": "",
          "textDictionary": {
            "catalog": "Catalog",
            "extensions": "Extensions"
          }
        },
        {
          "locale": "en",
          "textDictionary": {
            "catalog": "Catalog",
            "extensions": "Extensions"
          }
        },
        {
          "locale": "de",
          "textDictionary": {
            "catalog": "Katalog",
            "extensions": "Erweiterungen"
          }
        }
      ]
    }
  }
}`
}

func GetValidYAML_node_category_string() string {
	return `
name: overview2
luigiConfigFragment:
 data:
  nodeDefaults:
    entityType: global
    isolateView: true
  nodes:
  - entityType: global
    pathSegment: home
    label: Overview
    icon: home
    category: cat1
  texts:
  - locale: de
    textDictionary:
      hello: Hallo
`
}

func GetValidJSON_node_category_string() string {
	return `{
  "name": "overview2",
  "luigiConfigFragment": {
    "data": {
      "nodeDefaults": {
        "entityType": "global",
        "isolateView": true
      },
      "nodes": [
        {
          "entityType": "global",
          "pathSegment": "home",
          "label": "Overview",
          "icon": "home",
          "category": "cat1"
        }
      ],
      "texts": [
        {
          "locale": "de",
          "textDictionary": {
            "hello": "Hallo"
          }
        }
      ]
    }
  }
}`
}

func GetValidYAML_node_category_object() string {
	return `
name: overview2
luigiConfigFragment:
 data:
  nodeDefaults:
    entityType: global
    isolateView: true
  nodes:
  - entityType: global
    pathSegment: home
    label: Overview
    icon: home
    category:
      label: cat1
      icon: icon1
      collapsible: false
  texts:
  - locale: de
    textDictionary:
      hello: Hallo
`
}

func GetValidJSON_node_category_object() string {
	return `{
  "name": "overview2",
  "luigiConfigFragment": {
    "data": {
      "nodeDefaults": {
        "entityType": "global",
        "isolateView": true
      },
      "nodes": [
        {
          "entityType": "global",
          "pathSegment": "home",
          "label": "Overview",
          "icon": "home",
          "category": {
            "label": "cat1",
            "icon": "icon1",
            "collapsible": false
          }
        }
      ],
      "texts": [
        {
          "locale": "de",
          "textDictionary": {
            "hello": "Hallo"
          }
        }
      ]
    }
  }
}`
}

func GetInalidYAML_node_category_object() string {
	return `
name: overview2
luigiConfigFragment:
 data:
  nodeDefaults:
    entityType: global
    isolateView: true
  nodes:
  - entityType: global
    pathSegment: home
    label: Overview
    icon: home
    category:
      label: cat1
      icon: icon1
      collapsible: false
      invalidfield: invalid
  texts:
  - locale: de
    textDictionary:
      hello: Hallo
`
}

func GetValidYaml_targetAppConfig_viewGroup2() string {
	return `{
    "name": "extension-manager",
    "luigiConfigFragment": {
        "data": {
            "userSettings": {
                "groups": {
                    "user1": {
                        "label": "label",
                        "sublabel": "sublabel",
                        "title": "title",
                        "icon": "icon",
                        "viewUrl": "viewUrl",
                        "settings": {
                            "option1": {
                                "type": "type",
                                "label": "label",
                                "style": "style",
                                "options": [],
                                "isEditable": false
                            }
                        }
                    }
                }
            },
            "nodeDefaults": {
                "entityType": "type",
                "isolateView": false
            },
            "targetAppConfig": {
                "_version": "1.13.0",
                "sap.integration": {
                    "navMode": "inplace",
                    "urlTemplateId": "urltemplate.url",
                    "urlTemplateParams": {
                        "query": {}
                    }
                }
            },
            "viewGroup": {
                "preloadSuffix": "/#/preload"
            },
            "nodes": [
                {
                    "entityType": "global",
                    "pathSegment": "catalog",
                    "label": "{{catalog}}",
                    "icon": "business-one",
                    "dxpOrder": 6,
                    "order": 6,
                    "hideSideNav": true,
                    "tabNav": true,
                    "showBreadcrumbs": false,
                    "urlSuffix": "/#/global-catalog",
                    "visibleForFeatureToggles": [
                        "!global-catalog"
                    ]
                },
                {
                    "entityType": "global",
                    "pathSegment": "catalog",
                    "label": "{{catalog}}",
                    "icon": "business-one",
                    "dxpOrder": 6,
                    "order": 6,
                    "hideSideNav": true,
                    "tabNav": true,
                    "showBreadcrumbs": false,
                    "urlSuffix": "/#/new-global-catalog",
                    "visibleForFeatureToggles": [
                        "global-catalog"
                    ]
                },
                {
                    "entityType": "global",
                    "pathSegment": "extensions",
                    "label": "{{extensions}}",
                    "hideFromNav": true,
                    "children": [
                        {
                            "pathSegment": ":extClassName",
                            "hideFromNav": true,
                            "urlSuffix": "/#/extensions/:extClassName",
                            "context": {
                                "extClassName": ":extClassName"
                            }
                        }
                    ]
                }
            ],
            "texts": [
                {
                    "locale": "",
                    "textDictionary": {
                        "catalog": "Catalog",
                        "extensions": "Extensions"
                    }
                },
                {
                    "locale": "en",
                    "textDictionary": {
                        "catalog": "Catalog",
                        "extensions": "Extensions"
                    }
                },
                {
                    "locale": "de",
                    "textDictionary": {
                        "catalog": "Katalog",
                        "extensions": "Erweiterungen"
                    }
                }
            ]
        }
    }
}
`
}

func GetValidJSON_extension_manager_ui1() string {
	return `      {
        "name": "extension-manager",
        "luigiConfigFragment": {
          "data": {
            "viewGroup": {
              "preloadSuffix": "/#/preload"
            },
            "nodes": [
              {
                "pathSegment": "catalog",
                "label": "{{extensions}}",
                "icon": "cart",
                "entityType": "project",
                "navSlot": "settings",
                "dxpOrder": 10,
                "order": 10,
                "urlSuffix": "/#/catalog",
                "testId": "dxp-frame-navigation-project-extensions-catalog",
                "defineEntity": {
                  "id": "account",
                  "useBack": true
                },
                "keepSelectedForChildren": true
              },
              {
                "pathSegment": "catalog",
                "label": "{{extensions}}",
                "icon": "cart",
                "entityType": "team",
                "navSlot": "settings",
                "dxpOrder": 10,
                "order": 10,
                "urlSuffix": "/#/catalog",
                "testId": "dxp-frame-navigation-team-extensions-catalog",
                "defineEntity": {
                  "id": "account",
                  "useBack": true
                },
                "keepSelectedForChildren": true
              },
              {
                "entityType": "project.account",
                "pathSegment": "create-res/:scope/:extClassName/account/:accountType",
                "hideFromNav": true,
                "urlSuffix": "/#/extensions/:scope/:extClassName/account/:accountType/create-resource",
                "context": {
                  "extClassName": ":extClassName"
                }
              },
              {
                "entityType": "project.account",
                "pathSegment": "edit-res/:scope/:extClassName/account/:accountType/:name/:nspace",
                "hideFromNav": true,
                "urlSuffix": "/#/extensions/:scope/:extClassName/account/:accountType/edit-resource/:name/:nspace",
                "context": {
                  "extClassName": ":extClassName"
                }
              },
              {
                "entityType": "project",
                "pathSegment": "accounts",
                "hideFromNav": true,
                "urlSuffix": "/#/catalog",
                "context": {
                  "layout": "TwoColumnsMidExpanded",
                  "extClassName": "dxp-github-ui"
                }
              },
              {
                "entityType": "project",
                "pathSegment": "install-extensions",
                "hideFromNav": true,
                "urlSuffix": "/#/install-extensions"
              },
              {
                "entityType": "team",
                "pathSegment": "install-extensions",
                "hideFromNav": true,
                "urlSuffix": "/#/install-extensions"
              },
              {
                "pathSegment": "extension-missing-mandatory-data",
                "hideFromNav": true,
                "context": {
                  "providesMissingMandatoryDataUrl": true
                },
                "urlSuffix": "/#/extension-missing-mandatory-data/:extClassName",
                "entityType": "project",
                "testId": "dxp-frame-navigation-project-extension-missing-mandatory-data"
              },
              {
                "entityType": "project",
                "pathSegment": "extensions",
                "label": "{{extensions}}",
                "hideFromNav": true,
                "children": [
                  {
                    "pathSegment": ":extClassName",
                    "hideFromNav": true,
                    "urlSuffix": "/#/extensions/:extClassName",
                    "context": {
                      "extClassName": ":extClassName"
                    }
                  }
                ]
              },
              {
                "entityType": "team",
                "pathSegment": "extensions",
                "label": "{{extensions}}",
                "hideFromNav": true,
                "children": [
                  {
                    "pathSegment": ":extClassName",
                    "hideFromNav": true,
                    "urlSuffix": "/#/extensions/:extClassName",
                    "context": {
                      "extClassName": ":extClassName"
                    }
                  }
                ]
              },
              {
                "category": {
                  "id": "community-extensions",
                  "label": "{{communityExtensions}}",
                  "isGroup": true
                },
                "dxpOrder": 10,
                "order": 20,
                "entityType": "project"
              },
              {
                "category": {
                  "id": "community-extensions",
                  "label": "{{communityExtensions}}",
                  "isGroup": true
                },
                "dxpOrder": 20,
                "order": 20,
                "entityType": "project.component"
              }
            ],
            "texts": [
              {
                "locale": "",
                "textDictionary": {
                  "extensions": "Extensions",
                  "all": "All",
                  "communityExtensions": "Community Extensions"
                }
              },
              {
                "locale": "en",
                "textDictionary": {
                  "extensions": "Extensions",
                  "all": "All",
                  "communityExtensions": "Community Extensions"
                }
              },
              {
                "locale": "de",
                "textDictionary": {
                  "extensions": "Erweiterungen",
                  "all": "Alle",
                  "communityExtensions": "Community Erweiterungen"
                }
              }
            ]
          }
        }
      }
`
}

func GetValidJSON_github_ui1() string {
	return `      {
        "name": "github-ui",
        "luigiConfigFragment": {
            "data": {
                "viewGroup": {
                  "preloadSuffix": "/#/preload",
                  "requiredIFramePermissions": {
                    "allow": ["clipboard-read", "clipboard-write"]
                  }
                },
                "nodes": [
                  {
                    "pathSegment": "github-loading-screen",
                    "hideFromNav": true,
                    "urlSuffix": "/#/projects/:projectId/github-loading-screen",
                    "entityType": "project"
                  },
                  {
                    "pathSegment": "github",
                    "hideFromNav": true,
                    "urlSuffix": "/#/projects/:projectId/connect-account-dialog",
                    "entityType": "project.account"
                  },
                  {
                    "pathSegment": "github-loading-screen",
                    "hideFromNav": true,
                    "urlSuffix": "/#/teams/:teamId/github-loading-screen",
                    "entityType": "team"
                  },
                  {
                    "pathSegment": "github",
                    "hideFromNav": true,
                    "urlSuffix": "/#/teams/:teamId/connect-account-dialog",
                    "entityType": "team.account"
                  },
                  {
                    "pathSegment" : "github-code",
                    "label" : "Code",
                    "url": "{context.entityContext.component.annotations[\"github.dxp.sap.com/repo-url\"]}",
                    "virtualTree": false,
                    "isolateView": true,
                    "loadingIndicator": {
                      "enabled": false
                    },
                    "entityType": "project.component",
                    "icon": "source-code",
                    "visibleForPlugin": true,
                    "visibleForContext": "serviceProviderConfig.disableGithubCode == null  || serviceProviderConfig.disableGithubCode == 'false'",
                    "category": {
                      "label": "{{development}}",
                      "collapsable": false,
                      "dxpOrder": 100,
                      "order": 100
                    }
                  },
                  {
                    "pathSegment" : "github-pulls",
                    "label" : "Pulls",
                    "url": "{context.entityContext.component.annotations[\"github.dxp.sap.com/repo-url\"]}/pulls",
                    "virtualTree": false,
                    "isolateView": true,
                    "visibleForPlugin": true,
                    "visibleForContext": "serviceProviderConfig.disableGithubPullRequests == null  || serviceProviderConfig.disableGithubPullRequests == 'false'",
                    "loadingIndicator": {
                      "enabled": false
                    },
                    "entityType": "project.component",
                    "icon": "wrench",
                    "category": { "label": "{{development}}" }
                  },
                  {
                    "pathSegment" : "github-issues",
                    "label" : "Issues",
                    "url": "{context.entityContext.component.annotations[\"github.dxp.sap.com/repo-url\"]}/issues",
                    "virtualTree": false,
                    "isolateView": true,
                    "visibleForPlugin": true,
                    "visibleForContext": "serviceProviderConfig.disableGithubIssues == null  || serviceProviderConfig.disableGithubIssues == 'false'",
                    "loadingIndicator": {
                      "enabled": false
                    },
                    "entityType": "project.component",
                    "icon": "task",
                    "category": {
                      "label": "{{issueManagement}}",
                      "collapsable": false,
                      "dxpOrder": 200,
                      "order": 200
                    }
                  }
                ],
                "texts": [
                  {
                    "locale": "",
                    "textDictionary": {
                      "quality": "Security & Quality",
                      "issueManagement": "Issue Management",
                      "development": "Development"
                    }
                  },
                  {
                    "locale": "en",
                    "textDictionary": {
                      "quality": "Security & Quality",
                      "issueManagement": "Issue Management",
                      "development": "Development"
                    }
                  },
                  {
                    "locale": "de",
                    "textDictionary": {
                      "quality": "Sicherheit & Qualität",
                      "issueManagement": "Issue Management",
                      "development": "Development"
                    }
                  }
                ]
            }
        }
      }
`
}

func GetValidJSON_github_wc() string {
	return `      {
        "name": "github-wc",
        "luigiConfigFragment": {
            "data": {
                "viewGroup": {
                    "preloadSuffix": "/#/preload",
                    "requiredIFramePermissions": {
                      "allow": ["clipboard-read", "clipboard-write"]
                  }
                },
                "nodes": [
                  {
                    "entityType": "project.overview::compound",
                    "pathSegment": "add-github-account-card",
                    "urlSuffix": "/main.js#add-github-account-card",
                    "visibleForContext": "(serviceProviderConfig.skipOnboardingCard == null  || serviceProviderConfig.skipOnboardingCard == \"false\") && ( serviceProviderConfig.githubAccountAdded == null  || serviceProviderConfig.githubAccountAdded == \"false\")",
                    "visibleForEntityContext": {
                      "project": {
                        "policies": ["iamMember"]
                      }
                    },
                    "layoutConfig": {
                      "slot": "recommended-actions",
                      "order": 10
                    },
                    "webcomponent": {
                      "selfRegistered": true
                    }
                  }
                ],
                "texts": [
                  {
                    "locale": "",
                    "textDictionary": {
                      "quality": "Security & Quality",
                      "issueManagement": "Issue Management",
                      "development": "Development"
                    }
                  },
                  {
                    "locale": "en",
                    "textDictionary": {
                      "quality": "Security & Quality",
                      "issueManagement": "Issue Management",
                      "development": "Development"
                    }
                  },
                  {
                    "locale": "de",
                    "textDictionary": {
                      "quality": "Sicherheit & Qualität",
                      "issueManagement": "Issue Management",
                      "development": "Development"
                    }
                  }
                ]
            }
        }
      }
`
}

func GetValidJSON_iam_ui() string {
	return `{
        "name": "iam-ui",
        "luigiConfigFragment": {
          "data": {
            "viewGroup": {
              "preloadSuffix": "/#/preload",
              "requiredIFramePermissions": {
                "allow": ["clipboard-read", "clipboard-write"]
              }
            },
            "nodes": [
              {
                "entityType": "project",
                "pathSegment": "members",
                "label": "{{members}}",
                "icon": "company-view",
                "hideFromNav": false,
                "urlSuffix": "/#/projects/:projectId/members",
                "navSlot": "settings",
                "dxpOrder": 30,
                "order": 30
              },
              {
                "entityType": "project",
                "pathSegment": "add-members",
                "label": "{{members}}",
                "hideFromNav": true,
                "urlSuffix": "/#/projects/:projectId/add-members"
              },
              {
                "entityType": "team",
                "pathSegment": "members",
                "label": "{{members}}",
                "icon": "company-view",
                "hideFromNav": false,
                "urlSuffix": "/#/teams/:teamId/members",
                "navSlot": "settings",
                "dxpOrder": 30,
                "order": 30
              },
              {
                "entityType": "team",
                "pathSegment": "add-members",
                "label": "{{members}}",
                "hideFromNav": true,
                "urlSuffix": "/#/teams/:teamId/add-members"
              }
            ],
            "texts": [
              {
                "locale": "",
                "textDictionary": {
                  "members": "Members"
                }
              },
              {
                "locale": "en",
                "textDictionary": {
                  "members": "Members"
                }
              },
              {
                "locale": "de",
                "textDictionary": {
                  "members": "Mitglieder"
                }
              }
            ]
          }
        }
      }
`
}

func GetValidJSON_learnings() string {
	return `{
        "name": "learning",
        "luigiConfigFragment": {
              "data": {
                  "nodes": [
                    {
                      "pathSegment": "help-portal-documentation",
                      "label": "Documentation",
                      "icon": "document-text",
                      "entityType": "global",
                      "dxpOrder": 6,
                      "order": 6,
                      "url": "https://uacptraining.int.hana.ondemand.com/docs/HYPERSPACE",
                      "visibleForFeatureToggles": ["helpPortalDocumentation"],
                      "visibleForPlugin": true,
                      "networkVisibility": "internal",
                      "hideSideNav": true,
                      "virtualTree": false,
                      "isolateView": true
                    },
                    {
                      "pathSegment": "learning",
                      "label": "{{learning}}",
                      "icon": "education",
                      "entityType": "global",
                      "hideSideNav": true,
                      "dxpOrder": 5,
                      "order": 5,
                      "tabNav": true,
                      "showBreadcrumbs": false,
                      "children": [
                        {
                          "pathSegment": "home",
                          "label": "{{home}}",
                          "icon": "home",
                          "url": "about:blank",
                          "compound": {
                            "renderer": {
                              "use": "grid",
                              "config": {
                                "columns": "minmax(0,1fr) minmax(0,1fr) minmax(0,1fr)",
                                "rows": "[first] repeat(20, auto ) [last]",
                                "layouts": [
                                  {
                                    "minWidth": 0,
                                    "maxWidth": 600,
                                    "columns": "minmax(0,1fr)",
                                    "rows": "[first] repeat(20, auto ) [last]",
                                    "gap": "0px"
                                  },
                                  {
                                    "minWidth": 600,
                                    "maxWidth": 1024,
                                    "columns": "minmax(0,1fr) minmax(0,1fr)",
                                    "rows": "[first] repeat(20, auto ) [last]",
                                    "gap": "0px"
                                  }
                                ]
                              }
                            },
                            "children": [
                              {
                                "urlSuffix": "/microfrontends/feature.js",
                                "context": {
                                  "title": "Hyperspace Portal Documentation",
                                  "content": "Learn how to use the various functionalities of the Hyperspace Portal.",
                                  "alt_label": "{{hero_alt}}",
                                  "gradientColor1": "#02172D",
                                  "gradientColor2": "#203046",
                                  "build_button_label": "Read the documentation",
                                  "feature_link": "/learning/documentation"
                                },
                                "layoutConfig": {
                                  "slot": "content",
                                  "row": "first",
                                  "column": "1"
                                }
                              },
                              {
                                "urlSuffix": "/microfrontends/feature.js",
                                "context": {
                                  "title": "Hyperspace Academy",
                                  "content": "Learn how to use Hyperspace Paved Roads, tools, services, and apply community best practices.",
                                  "alt_label": "{{hero_alt}}",
                                  "gradientColor1": "#DB1F77",
                                  "gradientColor2": "#29313A",
                                  "build_button_label": "Visit the Hyperspace Academy",
                                  "feature_link": "https://pages.github.tools.sap/hyperspace/academy/"
                                },
                                "layoutConfig": {
                                  "slot": "content",
                                  "row": "first",
                                  "column": "2"
                                }
                              },
                              {
                                "urlSuffix": "/microfrontends/feature.js",
                                "context": {
                                  "title": "Hyperspace SharePoint",
                                  "content": "Get an overview of what Hyperspace is all about and stay informed about recent updates.",
                                  "alt_label": "{{hero_alt}}",
                                  "gradientColor1": "#57CC99",
                                  "gradientColor2": "#29313A",
                                  "build_button_label": "Visit the Hyperspace SharePoint",
                                  "feature_link": "https://sap.sharepoint.com/sites/124706"
                                },
                                "layoutConfig": {
                                  "slot": "content",
                                  "row": "first",
                                  "column": "3"
                                }
                              },
                              {
                                "urlSuffix": "/microfrontends/quicklinks.js",
                                "context": {
                                  "title": "Get Going",
                                  "description": "Helpful links in the context of the Hyperspace Portal",
                                  "links": [
                                    {
                                      "label": "User guide",
                                      "url": "https://portal.hyperspace.tools.sap/projects/dxp/documentation/User-Guide/Getting-Started/Overview"
                                    },
                                    {
                                      "label": "Extension catalog",
                                      "url": "https://portal.hyperspace.tools.sap/projects/dxp/documentation/Extension-Catalog"
                                    },
                                    {
                                      "label": "How to contribute",
                                      "url": "https://portal.hyperspace.tools.sap/projects/dxp/documentation/Extend-&-Contribute/Contribution-Guidelines/Overview"
                                    }
                                  ]
                                },
                                "layoutConfig": {
                                  "slot": "content"
                                }
                              },
                              {
                                "urlSuffix": "/microfrontends/quicklinks.js",
                                "context": {
                                  "title": "Find Guidance",
                                  "description": "Relevant links within the Hyperspace Academy",
                                  "links": [
                                    {
                                      "label": "Paved Roads",
                                      "url": "https://pages.github.tools.sap/hyperspace/academy/pavedroad/"
                                    },
                                    {
                                      "label": "Tools documentation",
                                      "url": "https://pages.github.tools.sap/hyperspace/academy/tools/"
                                    },
                                    {
                                      "label": "Service documentations",
                                      "url": "https://pages.github.tools.sap/hyperspace/academy/services/"
                                    },
                                    {
                                      "label": "Community-driven content",
                                      "url": "https://pages.github.tools.sap/hyperspace/academy/communitycontent/"
                                    }
                                  ]
                                },
                                "layoutConfig": {
                                  "slot": "content"
                                }
                              },
                              {
                                "urlSuffix": "/microfrontends/quicklinks.js",
                                "context": {
                                  "title": "Learn More",
                                  "description": "Relevant links within the Hyperspace SharePoint",
                                  "links": [
                                    {
                                      "label": "Development Platform offerings",
                                      "url": "https://sap.sharepoint.com/sites/124706/SitePages/Hyperspace-Development-Platform.aspx"
                                    },
                                    {
                                      "label": "What's next (roadmap)",
                                      "url": "https://sap.sharepoint.com/sites/124706/SitePages/What's-next-(roadmap).aspx"
                                    },
                                    {
                                      "label": "What's new (release notes)",
                                      "url": "https://sap.sharepoint.com/sites/124706/SitePages/What's-new-(release-notes).aspx"
                                    },
                                    {
                                      "label": "Join events & communities",
                                      "url": "https://sap.sharepoint.com/sites/124706/SitePages/Join-Events.aspx"
                                    }
                                  ]
                                },
                                "layoutConfig": {
                                  "slot": "content"
                                }
                              },
                              {
                                "urlSuffix": "/microfrontends/quicklinks.js",
                                "context": {
                                  "title": "Ask a Question",
                                  "description": "Link to search stack",
                                  "links": [
                                    {
                                      "label": "Help Center",
                                      "url": "https://portal.d1.hyperspace.tools.sap/projects/hyperspace-academy-experimental-laboratory/documentation/Home?modal=%2Fhelp&modalParams=%7B%22size%22:%22fullscreen%22%7D"
                                    }
                                  ]
                                },
                                "layoutConfig": {
                                  "slot": "content"
                                }
                              },
                              {
                                "urlSuffix": "/microfrontends/quicklinks.js",
                                "context": {
                                  "title": "Get Support",
                                  "description": "Links to the tools support channels",
                                  "links": [
                                    {
                                      "label": "Tool support channels",
                                      "url": "https://pages.github.tools.sap/hyperspace/academy/tools/"
                                    }
                                  ]
                                },
                                "layoutConfig": {
                                  "slot": "content"
                                }
                              }
                            ]
                          }
                        },
                        {
                          "pathSegment": "documentation",
                          "hideFromNav": true,
                          "url": "https://md-html.portal.{context.serviceProviderConfig.clusterHost}/#/",
                          "context": {
                            "projectId": "dxp"
                          },
                          "virtualTree": true
                        },
                        {
                          "pathSegment": "goldenPath",
                          "label": "{{goldenPath}}",
                          "url": "https://pages-ght.{context.serviceProviderConfig.clusterHost}/dxp/golden-path/",
                          "requiredIFramePermissions": {
                            "allow": ["clipboard-read", "clipboard-write", "fullscreen"]
                          },
                          "visibleForFeatureToggles": ["gp"],
                          "context": {
                            "pages": {
                              "login": "dxp",
                              "repoName": "golden-path"
                            }
                          },
                          "virtualTree": true,
                          "clientPermissions": {
                            "urlParameters": {
                              "url": {
                                "read": true,
                                "write": true
                              }
                            }
                          }
                        }
                      ]
                    }
                  ],
                  "texts": [
                    {
                      "locale": "",
                      "textDictionary": {
                        "home": "Home",
                        "hero_title": "Learn how Hyperspace Portal can help you build solutions",
                        "hero_content": "Explore projects, re-use templates and deploy your first components.",
                        "hero_alt": "Or you can...",
                        "hero_button_build": "Let's go build something",
                        "hero_button_docs": "Read Docs",
                        "learning": "Learning",
                        "dxp": "Hyperspace Portal",
                        "hyperspace": "Hyperspace",
                        "introduction": "Introduction",
                        "academy": "Academy",
                        "goldenPath": "Golden Path"
                      }
                    },
                    {
                      "locale": "en",
                      "textDictionary": {
                        "home": "Home",
                        "hero_title": "Learn how Hyperspace Portal can help you build solutions",
                        "hero_content": "Explore projects, re-use templates and deploy your first components.",
                        "hero_alt": "Or you can...",
                        "hero_button_build": "Let's go build something",
                        "hero_button_docs": "Read Docs",
                        "learning": "Learning",
                        "dxp": "Hyperspace Portal",
                        "hyperspace": "Hyperspace",
                        "introduction": "Introduction",
                        "academy": "Academy",
                        "goldenPath": "Golden Path"
                      }
                    },
                    {
                      "locale": "de",
                      "textDictionary": {
                        "home": "Home",
                        "hero_title": "Lerne, wie Hyperspace Portal bei der Erstellung von Lösungen helfen kann",
                        "hero_content": "Erkunde Projekte, verwende Templates und entwickle Deine ersten Komponenten",
                        "hero_alt": "oder...",
                        "hero_button_build": "Erschaffe etwas",
                        "hero_button_docs": "Lies die Dokumentation",
                        "learning": "Learning",
                        "dxp": "Hyperspace Portal",
                        "hyperspace": "Hyperspace",
                        "introduction": "Einführung",
                        "academy": "Akademie",
                        "goldenPath": "Golden Path"
                      }
                    }
                  ]
              }
          }
      }`
}

func GetValidJSON_organization_ui() string {
	return `{
        "name": "organization-ui",
        "luigiConfigFragment": {
          "data": {
            "viewGroup": {
              "preloadSuffix": "/#/preload",
              "requiredIFramePermissions": {
                "allow": ["clipboard-read", "clipboard-write"]
              }
            },
            "nodes": [
              {
                "entityType": "global",
                "pathSegment": "products",
                "label": "{{products}}",
                "hideSideNav": true,
                "icon": "product",
                "urlSuffix": "/{i18n.currentLocale}/#/products",
                "dxpOrder": 2,
                "navigationContext": "projects",
                "visibleForFeatureToggles": ["splitProjectByType"]
              },
              {
                "entityType": "global",
                "pathSegment": "experiments",
                "label": "{{experiments}}",
                "hideSideNav": true,
                "icon": "lab",
                "urlSuffix": "/{i18n.currentLocale}/#/experiments",
                "dxpOrder": 2.1,
                "navigationContext": "projects",
                "visibleForFeatureToggles": ["splitProjectByType"]
              },
              {
                "entityType": "global",
                "pathSegment": "projects",
                "label": "{{projects}}",
                "hideSideNav": true,
                "icon": "curriculum",
                "urlSuffix": "/{i18n.currentLocale}/#/projects",
                "dxpOrder": 2,
                "navigationContext": "projects",
                "visibleForFeatureToggles": ["!splitProjectByType"],
                "context": {
                  "_tmpUseBreadcrumbsForTitle": true
                },
                "children": [
                  {
                    "pathSegment": ":projectId",
                    "hideFromNav": true,
                    "navHeader": {
                      "useTitleResolver": true
                    },
                    "titleResolver": {
                      "request": {
                        "method": "GET",
                        "url": "${frameContext.accountSearchServiceApiUrl}?q=name:${projectId}",
                        "headers": {
                          "authorization": "Bearer ${token}"
                        }
                      },
                      "titlePropertyChain": "docs[0].displayName",
                      "prerenderFallback": false,
                      "fallbackTitle": "{{project}}",
                      "fallbackIcon": "curriculum"
                    },
                    "defineEntity": {
                      "id": "project",
                      "contextKey": "projectId",
                      "dynamicFetchId": "project",
                      "useBack": true,
                      "label": "{{project}}",
                      "pluralLabel": "{{projects}}",
                      "notFoundConfig": {
                        "entityListNavigationContext": "projects",
                        "sapIllusSVG": "Scene-NoSearchResults"
                      }
                    },
                    "context": {
                      "projectId": ":projectId"
                    },
                    "navigationContext": "project",
                    "children": [
                      {
                        "defineSlot": "main"
                      },
                      {
                        "defineSlot": ""
                      },
                      {
                        "defineSlot": "devopsMetrics",
                        "category": {
                          "label": "{{devopsMetrics}}",
                          "collapsible": false
                        }
                      },
                      {
                        "defineSlot": ""
                      },
                      {
                        "defineSlot": "settings",
                        "category": {
                          "label": "{{settings}}",
                          "collapsible": false
                        }
                      }
                    ]
                  }
                ]
              },
              {
                "entityType": "global",
                "pathSegment": "projects",
                "label": "{{projects}}",
                "hideFromNav": true,
                "hideSideNav": true,
                "urlSuffix": "/{i18n.currentLocale}/#/products",
                "navigationContext": "projects",
                "visibleForFeatureToggles": ["splitProjectByType"],
                "children": [
                  {
                    "pathSegment": ":projectId",
                    "hideFromNav": true,
                    "navHeader": {
                      "useTitleResolver": true
                    },
                    "titleResolver": {
                      "request": {
                        "method": "GET",
                        "url": "${frameContext.accountSearchServiceApiUrl}?q=name:${projectId}",
                        "headers": {
                          "authorization": "Bearer ${token}"
                        }
                      },
                      "titlePropertyChain": "docs[0].displayName",
                      "prerenderFallback": false,
                      "fallbackTitle": "{{project}}",
                      "fallbackIcon": "curriculum"
                    },
                    "defineEntity": {
                      "id": "project",
                      "contextKey": "projectId",
                      "dynamicFetchId": "project",
                      "useBack": true,
                      "label": "{{project}}",
                      "pluralLabel": "{{projects}}",
                      "notFoundConfig": {
                        "entityListNavigationContext": "projects",
                        "sapIllusSVG": "Scene-NoSearchResults"
                      }
                    },
                    "context": {
                      "projectId": ":projectId"
                    },
                    "navigationContext": "project",
                    "children": [
                      {
                        "defineSlot": "main"
                      },
                      {
                        "defineSlot": ""
                      },
                      {
                        "defineSlot": "devopsMetrics",
                        "category": {
                          "label": "{{devopsMetrics}}",
                          "collapsible": false
                        }
                      },
                      {
                        "defineSlot": ""
                      },
                      {
                        "defineSlot": "settings",
                        "category": {
                          "label": "{{settings}}",
                          "isGroup": true
                        }
                      }
                    ]
                  }
                ]
              },
              {
                "entityType": "global",
                "pathSegment": "teams",
                "label": "{{teams}}",
                "urlSuffix": "/{i18n.currentLocale}/#/teams",
                "hideSideNav": true,
                "icon": "group",
                "dxpOrder": 3,
                "navigationContext": "teams",
                "children": [
                  {
                    "pathSegment": ":teamId",
                    "hideFromNav": true,
                    "navHeader": {
                      "useTitleResolver": true
                    },
                    "titleResolver": {
                      "request": {
                        "method": "GET",
                        "url": "${frameContext.accountSearchServiceApiUrl}?q=name:${teamId}&fuzzy=false&fq=accountRole%3A%22Team%22",
                        "headers": {
                          "authorization": "Bearer ${token}"
                        }
                      },
                      "titlePropertyChain": "docs[0].displayName",
                      "prerenderFallback": false,
                      "fallbackTitle": "{{team}}",
                      "fallbackIcon": "group"
                    },
                    "defineEntity": {
                      "id": "team",
                      "contextKey": "teamId",
                      "dynamicFetchId": "team",
                      "useBack": true,
                      "label": "{{team}}",
                      "pluralLabel": "{{teams}}",
                      "notFoundConfig": {
                        "entityListNavigationContext": "teams",
                        "sapIllusSVG": "Scene-NoSearchResults"
                      }
                    },
                    "context": {
                      "teamId": ":teamId"
                    },
                    "navigationContext": "team",
                    "children": [
                      {
                        "defineSlot": "main"
                      },
                      {
                        "defineSlot": ""
                      },
                      {
                        "defineSlot": "settings",
                        "category": {
                          "label": "{{settings}}",
                          "collapsible": false
                        }
                      }
                    ]
                  }
                ]
              },
              {
                "pathSegment": "projects-create-dialog",
                "entityType": "global",
                "hideFromNav": true,
                "hideSideNav": true,
                "navigationContext": "projects",
                "context": {
                  "_tmpUseBreadcrumbsForTitle": true
                },
                "urlSuffix": "/{i18n.currentLocale}/#/projects-create-dialog"
              },
              {
                "pathSegment": "create-product",
                "entityType": "global",
                "hideFromNav": true,
                "hideSideNav": true,
                "label": "{{createProduct}}",
                "visibleForFeatureToggles": ["splitProjectByType"],
                "urlSuffix": "/{i18n.currentLocale}/#/create-project?type=product"
              },
              {
                "pathSegment": "create-experiment",
                "entityType": "global",
                "hideFromNav": true,
                "hideSideNav": true,
                "label": "{{createExperiment}}",
                "visibleForFeatureToggles": ["splitProjectByType"],
                "urlSuffix": "/{i18n.currentLocale}/#/create-project?type=experiment"
              },
              {
                "pathSegment": "create-project",
                "entityType": "global",
                "hideFromNav": true,
                "hideSideNav": true,
                "context": {
                  "_tmpUseBreadcrumbsForTitle": true
                },
                "urlSuffix": "/{i18n.currentLocale}/#/choose-project-type"
              },
              {
                "pathSegment": "create-project-details",
                "entityType": "global",
                "hideFromNav": true,
                "hideSideNav": true,
                "context": {
                  "_tmpUseBreadcrumbsForTitle": true
                },
                "urlSuffix": "/{i18n.currentLocale}/#/create-project"
              },
              {
                "pathSegment": "edit-project",
                "entityType": "project",
                "hideFromNav": true,
                "hideSideNav": true,
                "label": "Edit Project",
                "context": {
                  "_tmpUseBreadcrumbsForTitle": true
                },
                "urlSuffix": "/{i18n.currentLocale}/#/edit-project"
              },
              {
                "pathSegment": "edit-team",
                "entityType": "team",
                "hideFromNav": true,
                "hideSideNav": true,
                "label": "Edit Team",
                "context": {
                  "_tmpUseBreadcrumbsForTitle": true
                },
                "urlSuffix": "/{i18n.currentLocale}/#/edit-team"
              },
              {
                "pathSegment": "create-team",
                "entityType": "global",
                "hideFromNav": true,
                "hideSideNav": true,
                "label": "{{createTeam}}",
                "context": {
                  "_tmpUseBreadcrumbsForTitle": true
                },
                "urlSuffix": "/{i18n.currentLocale}/#/create-team"
              }
            ],
            "texts": [
              {
                "locale": "",
                "textDictionary": {
                  "projects": "Projects",
                  "project": "Project",
                  "products": "Products",
                  "experiments": "Experiments",
                  "createProduct": "Create Product",
                  "createExperiment": "Create Experiment",
                  "createTeam": "Create Team",
                  "teams": "Teams",
                  "team": "Team",
                  "settings": "Settings & Accounts",
                  "devopsMetrics": "DevOps Metrics"
                }
              },
              {
                "locale": "en",
                "textDictionary": {
                  "projects": "Projects",
                  "project": "Project",
                  "products": "Products",
                  "experiments": "Experiments",
                  "createProduct": "Create Product",
                  "createExperiment": "Create Experiment",
                  "createTeam": "Create Team",
                  "teams": "Teams",
                  "team": "Team",
                  "settings": "Settings & Accounts",
                  "devopsMetrics": "DevOps Metrics"
                }
              },
              {
                "locale": "de",
                "textDictionary": {
                  "projects": "Projekte",
                  "project": "Projekt",
                  "products": "Produkte",
                  "experiments": "Experimente",
                  "createProduct": "Produkt erstellen",
                  "createExperiment": "Experiment erstellen",
                  "createTeam": "Team erstellen",
                  "teams": "Teams",
                  "team": "Team",
                  "settings": "Einstellungen & Accounts",
                  "devopsMetrics": "DevOps Metrics"
                }
              }
            ]
          }
        }
      }`
}

func GetValidJSON_search_ui() string {
	return `{
        "name": "search-ui",
        "luigiConfigFragment": {
          "data": {
            "viewGroup": {
              "preloadSuffix": "/#/preload",
              "requiredIFramePermissions": {
                "allow": ["clipboard-read", "clipboard-write"],
                "sandbox": ["allow-forms"]
              }
            },
            "nodes": [
              {
                "entityType": "global",
                "pathSegment": "search",
                "hideFromNav": true,
                "showBreadcrumbs": false,
                "navHeader": {
                  "label": "Category"
                },
                "navigationContext": "search",
                "urlSuffix": "/#/search",
                "children": [
                  {
                    "label": "Projects {viewGroupData.projects}",
                    "icon": "curriculum",
                    "pathSegment": "projects",
                    "urlSuffix": "/#/search/projects",
                    "visibleForFeatureToggles": ["!splitProjectByType"],
                    "clientPermissions": {
                      "urlParameters": {
                        "q": {
                          "read": true,
                          "write": true
                        }
                      }
                    }
                  },
                  {
                    "label": "Products {viewGroupData.products}",
                    "icon": "product",
                    "pathSegment": "products",
                    "urlSuffix": "/#/search/products",
                    "visibleForFeatureToggles": ["splitProjectByType"],
                    "clientPermissions": {
                      "urlParameters": {
                        "q": {
                          "read": true,
                          "write": true
                        }
                      }
                    }
                  },
                  {
                    "hideFromNav": true,
                    "pathSegment": "projects",
                    "urlSuffix": "/#/search/products",
                    "visibleForFeatureToggles": ["splitProjectByType"],
                    "clientPermissions": {
                      "urlParameters": {
                        "q": {
                          "read": true,
                          "write": true
                        }
                      }
                    }
                  },
                  {
                    "label": "Experiments {viewGroupData.experiments}",
                    "icon": "lab",
                    "pathSegment": "experiments",
                    "urlSuffix": "/#/search/experiments",
                    "visibleForFeatureToggles": ["splitProjectByType"],
                    "clientPermissions": {
                      "urlParameters": {
                        "q": {
                          "read": true,
                          "write": true
                        }
                      }
                    }
                  },
                  {
                    "label": "Components {viewGroupData.components}",
                    "icon": "course-book",
                    "pathSegment": "components",
                    "urlSuffix": "/#/search/components",
                    "clientPermissions": {
                      "urlParameters": {
                        "q": {
                          "read": true,
                          "write": true
                        }
                      }
                    }
                  },
                  {
                    "label": "Teams {viewGroupData.teams}",
                    "icon": "group",
                    "pathSegment": "teams",
                    "urlSuffix": "/#/search/teams",
                    "clientPermissions": {
                      "urlParameters": {
                        "q": {
                          "read": true,
                          "write": true
                        }
                      }
                    }
                  },
                  {
                    "label": "Documentation {viewGroupData.techdocs}",
                    "icon": "curriculum",
                    "pathSegment": "techdocs",
                    "urlSuffix": "/#/search/techdocs",
                    "clientPermissions": {
                      "urlParameters": {
                        "q": {
                          "read": true,
                          "write": true
                        },
                        "url": {
                          "read": true,
                          "write": true
                        }
                      }
                    }
                  },
                  {
                    "label": "API Documentation (draft)",
                    "icon": "curriculum",
                    "pathSegment": "apidocs",
                    "urlSuffix": "/#/search/apidocs",
                    "visibleForFeatureToggles": ["enable-api-docs-search"],
                    "clientPermissions": {
                      "urlParameters": {
                        "q": {
                          "read": true,
                          "write": true
                        }
                      }
                    }
                  },
                  {
                    "label": "Users {viewGroupData.users}",
                    "icon": "group",
                    "pathSegment": "users",
                    "urlSuffix": "/#/search/users",
                    "clientPermissions": {
                      "urlParameters": {
                        "q": {
                          "read": true,
                          "write": true
                        }
                      }
                    }
                  }
                ]
              }
            ]
          }
        }
      }`
}

func GetValidJSON_extension_manager_ui2() string {
	return `{
        "name": "metadata-registry-ui",
        "luigiConfigFragment": {
              "data": {
                  "viewGroup": {
                    "preloadSuffix": "/#/preload",
                    "requiredIFramePermissions": {
                      "allow": ["clipboard-read", "clipboard-write"]
                    }
                  },
                  "nodes": [
                    {
                      "entityType": "project",
                      "pathSegment": "components",
                      "testId": "dxp-frame-navigation-project-components",
                      "label": "{{components}}",
                      "hideFromNav": false,
                      "navigationContext": "components",
                      "icon": "tree",
                      "keepSelectedForChildren": true,
                      "urlSuffix": "/#/components",
                      "ignoreInDocumentTitle": true,
                      "dxpOrder": 2,
                      "order": 2,
                      "children": [
                        {
                          "pathSegment": ":componentId",
                          "hideFromNav": true,
                          "label": "{{components}}",
                          "icon": "tree",
                          "keepSelectedForChildren": false,
                          "navigationContext": "component",
                          "navHeader": {
                            "useTitleResolver": true,
                            "showUpLink": false
                          },
                          "titleResolver": {
                            "request": {
                              "method": "GET",
                              "url": "${frameContext.componentSearchServiceApiUrl}?q=account:${projectId}%20name:${componentId}",
                              "headers": {
                                "authorization": "Bearer ${token}"
                              }
                            },
                            "titlePropertyChain": "docs[0].displayName",
                            "prerenderFallback": false,
                            "fallbackTitle": "{{component}}",
                            "fallbackIcon": "tree"
                          },
                          "defineEntity": {
                            "id": "component",
                            "useBack": true,
                            "contextKey": "componentId",
                            "dynamicFetchId": "component",
                            "label": "{{component}}",
                            "pluralLabel": "{{components}}",
                            "notFoundConfig": {
                              "entityListNavigationContext": "components",
                              "sapIllusSVG": "Scene-NoSearchResults"
                            }
                          },
                          "context": {
                            "componentId": ":componentId"
                          }
                        }
                      ]
                    },
                    {
                      "entityType": "team",
                      "pathSegment": "components",
                      "testId": "dxp-frame-navigation-team-components",
                      "label": "{{components}}",
                      "hideFromNav": false,
                      "navigationContext": "components",
                      "icon": "tree",
                      "keepSelectedForChildren": true,
                      "urlSuffix": "/#/components",
                      "ignoreInDocumentTitle": true,
                      "dxpOrder": 2,
                      "order": 2,
                      "children": [
                        {
                          "pathSegment": ":componentId",
                          "hideFromNav": true,
                          "label": "{{components}}",
                          "icon": "tree",
                          "keepSelectedForChildren": false,
                          "navigationContext": "component",
                          "navHeader": {
                            "useTitleResolver": true,
                            "showUpLink": false
                          },
                          "titleResolver": {
                            "request": {
                              "method": "GET",
                              "url": "${frameContext.componentSearchServiceApiUrl}?q=teamName:${teamId}%20name:${componentId}",
                              "headers": {
                                "authorization": "Bearer ${token}"
                              }
                            },
                            "titlePropertyChain": "docs[0].displayName",
                            "prerenderFallback": false,
                            "fallbackTitle": "{{component}}",
                            "fallbackIcon": "tree"
                          },
                          "defineEntity": {
                            "id": "component",
                            "useBack": true,
                            "contextKey": "componentId",
                            "dynamicFetchId": "component",
                            "label": "{{component}}",
                            "pluralLabel": "{{components}}",
                            "notFoundConfig": {
                              "entityListNavigationContext": "components",
                              "sapIllusSVG": "Scene-NoSearchResults"
                            }
                          },
                          "context": {
                            "componentId": ":componentId"
                          }
                        }
                      ]
                    },
                    {
                      "pathSegment": "add-component",
                      "entityType": "project",
                      "urlSuffix": "/#/projects/:projectId/add-component",
                      "hideFromNav": true
                    },
                    {
                      "pathSegment": "create-component",
                      "entityType": "project",
                      "hideFromNav": true,
                      "hideSideNav": true,
                      "context": {
                        "_tmpUseBreadcrumbsForTitle": true
                      },
                      "urlSuffix": "/#/projects/:projectId/create-component"
                    },
                    {
                      "entityType": "project",
                      "pathSegment": "bounded-contexts",
                      "label": "Bounded Contexts",
                      "hideFromNav": true,
                      "showBreadcrumbs": false,
                      "children": [
                        {
                          "pathSegment": ":boundedContextId",
                          "hideFromNav": true,
                          "navHeader": {
                            "label": "Bounded Context",
                            "icon": "tree"
                          },
                          "defineEntity": {
                            "id": "boundedContext",
                            "useBack": true,
                            "contextKey": "boundedContextId",
                            "label": "Bounded Context",
                            "pluralLabel": "Bounded Contexts"
                          },
                          "context": {
                            "boundedContextId": ":boundedContextId"
                          }
                        }
                      ]
                    },
                    {
                      "entityType": "project",
                      "pathSegment": "api-definitions",
                      "label": "API Definitions",
                      "hideFromNav": true,
                      "showBreadcrumbs": false,
                      "children": [
                        {
                          "pathSegment": ":apiDefinitionId",
                          "hideFromNav": true,
                          "navHeader": {
                            "label": "API Definition",
                            "icon": "tree"
                          },
                          "defineEntity": {
                            "id": "apiDefinition",
                            "useBack": true,
                            "contextKey": "apiDefinitionId",
                            "label": "API Definition",
                            "pluralLabel": "API Definitions"
                          },
                          "context": {
                            "apiDefinitionId": ":apiDefinitionId"
                          }
                        }
                      ]
                    }
                  ],
                  "texts": [
                    {
                      "locale": "",
                      "textDictionary": {
                        "components": "Components",
                        "component": "Component"
                      }
                    },
                    {
                      "locale": "en",
                      "textDictionary": {
                        "components": "Components",
                        "component": "Component"
                      }
                    },
                    {
                      "locale": "de",
                      "textDictionary": {
                        "components": "Komponenten",
                        "component": "Komponente"
                      }
                    }
                  ]
              }
          }
      }`
}

func GetValidJSON_metadata_registry_wc() string {
	return `{
        "name": "metadata-registry-wc",
        "luigiConfigFragment": {
              "data": {
                  "nodes": [
                  {
                    "entityType": "project.overview::compound",
                    "pathSegment": "team-sidebar",
                    "urlSuffix": "/main.js#team-sidebar",
                    "layoutConfig": {
                      "slot": "sidebar",
                      "order": 20
                    },
                    "webcomponent": {
                      "selfRegistered": true
                    }
                  },
                  {
                    "entityType": "project.overview::compound",
                    "pathSegment": "main-contacts-sidebar",
                    "urlSuffix": "/main.js#main-contacts-sidebar",
                    "layoutConfig": {
                      "slot": "sidebar",
                      "order": 30
                    },
                    "webcomponent": {
                      "selfRegistered": true
                    }
                  },
                  {
                    "entityType": "project.overview::compound",
                    "pathSegment": "docs-sidebar",
                    "urlSuffix": "/main.js#docs-sidebar",
                    "layoutConfig": {
                      "slot": "sidebar",
                      "order": 40
                    },
                    "webcomponent": {
                      "selfRegistered": true
                    }
                  },
                  {
                    "entityType": "project.overview::compound",
                    "pathSegment": "channels",
                    "urlSuffix": "/main.js#channels-sidebar",
                    "layoutConfig": {
                      "slot": "sidebar",
                      "order": 50
                    },
                    "webcomponent": {
                      "selfRegistered": true
                    }
                  },
                  {
                    "entityType": "project.component",
                    "pathSegment": "overview",
                    "label": "Overview",
                    "icon": "tree",
                    "dxpOrder": 1,
                    "order": 1,
                    "defineEntity": {
                      "id": "overview"
                    },
                    "webcomponent": true,
                    "url": "{context.serviceProviderConfig.homeBaseUrl}/microfrontends/dynamicPageDashboard.js",
                    "context": {
                      "columns": [
                        {
                          "max": "655px",
                          "layout": "minmax(0,1fr)",
                          "ignoreItemLayout": true
                        },
                        {
                          "min": "656px",
                          "max": "975px",
                          "layout": "minmax(0,1fr)",
                          "ignoreItemLayout": true
                        },
                        {
                          "min": "976px",
                          "max": "1359px",
                          "layout": "minmax(0,1fr) minmax(0,1fr)"
                        },
                        {
                          "min": "1360px",
                          "max": "1679px",
                          "layout": "minmax(0,1fr) minmax(0,1fr) minmax(0,1fr)"
                        },
                        {
                          "min": "1680px",
                          "max": "1999px",
                          "layout": "minmax(0,1fr) minmax(0,1fr) minmax(0,1fr) minmax(0,1fr)"
                        },
                        {
                          "min": "2000px",
                          "layout": "minmax(0,1fr) minmax(0,1fr) minmax(0,1fr) minmax(0,1fr) minmax(0,1fr)"
                        }
                      ],
                      "rows": "[first] repeat(20, auto ) [last]",
                      "dashboardPersistencePrefix": "dxp_component_overview",
                      "headerCollapseHeight": "50px",
                      "itemMargin": "10px"
                    },
                    "compound": {
                      "children": []
                    }
                  },
                  {
                    "entityType": "project.component.overview::compound",
                    "pathSegment": "component-overview-header",
                    "urlSuffix": "/main.js#component-overview-header",
                    "layoutConfig": {
                      "slot": "header"
                    },
                    "webcomponent": {
                      "selfRegistered": true
                    }
                  },
                  {
                    "entityType": "project.component.overview::compound",
                    "pathSegment": "repository-card",
                    "urlSuffix": "/main.js#repository-card",
                    "dxpOrder": 1,
                    "order": 1,
                    "layoutConfig": {
                      "slot": "content",
                      "column": "1 / span 2",
                      "row": "first"
                    },
                    "webcomponent": {
                      "selfRegistered": true
                    }
                  },
                  {
                    "entityType": "project.component.overview::compound",
                    "pathSegment": "sap-web-components-paved-road-card",
                    "urlSuffix": "/main.js#sap-web-components-paved-road-card",
                    "dxpOrder": 1,
                    "order": 1,
                    "layoutConfig": {
                      "slot": "content"
                    },
                    "webcomponent": {
                      "selfRegistered": true
                    },
                    "visibleForFeatureToggles": ["sap-web-components-paved-road"],
                    "visibleForContext": "contains(entityContext.component.tags, 'sap-webcomponent')"
                  },
                  {
                    "entityType": "project.component.overview::compound",
                    "pathSegment": "build-checks-card",
                    "urlSuffix": "/main.js#build-checks-card",
                    "dxpOrder": 2,
                    "order": 2,
                    "layoutConfig": {
                      "slot": "content"
                    },
                    "webcomponent": {
                      "selfRegistered": true
                    }
                  },
                  {
                    "entityType": "project.component.overview::compound",
                    "pathSegment": "links-card",
                    "urlSuffix": "/main.js#links-card",
                    "dxpOrder": 3,
                    "order": 3,
                    "layoutConfig": {
                      "slot": "content"
                    },
                    "webcomponent": {
                      "selfRegistered": true
                    }
                  },
                  {
                    "entityType": "project.overview::compound",
                    "pathSegment": "add-component-card",
                    "urlSuffix": "/main.js#add-component-card",
                    "visibleForContext": "(serviceProviderConfig.skipOnboardingCard == null || serviceProviderConfig.skipOnboardingCard == \"false\") && ( serviceProviderConfig.componentCreated == null || serviceProviderConfig.componentCreated == \"false\")",
                    "visibleForEntityContext": {
                      "project": {
                        "policies": ["iamMember"]
                      }
                    },
                    "layoutConfig": {
                      "slot": "recommended-actions",
                      "order": 20
                    },
                    "webcomponent": {
                      "selfRegistered": true
                    }
                  },
                  {
                    "entityType": "project.overview::compound",
                    "pathSegment": "links-card",
                    "urlSuffix": "/main.js#links-card",
                    "layoutConfig": {
                      "slot": "content",
                      "order": 20
                    },
                    "webcomponent": {
                      "selfRegistered": true
                    }
                  },
                  {
                    "entityType": "project.boundedContext.overview::compound",
                    "pathSegment": "links-card",
                    "urlSuffix": "/main.js#links-card",
                    "dxpOrder": 3,
                    "order": 3,
                    "layoutConfig": {
                      "slot": "content"
                    },
                    "webcomponent": {
                      "selfRegistered": true
                    }
                  },
                  {
                    "entityType": "project.apiDefinition.overview::compound",
                    "pathSegment": "links-card",
                    "urlSuffix": "/main.js#links-card",
                    "dxpOrder": 3,
                    "order": 3,
                    "layoutConfig": {
                      "slot": "content"
                    },
                    "webcomponent": {
                      "selfRegistered": true
                    }
                  },
                  {
                    "entityType": "project.component.overview::compound",
                    "pathSegment": "pipeline-card",
                    "urlSuffix": "/main.js#pipeline-card",
                    "dxpOrder": 4,
                    "order": 4,
                    "layoutConfig": {
                      "slot": "content"
                    },
                    "webcomponent": {
                      "selfRegistered": true
                    }
                  },
                  {
                    "entityType": "project.component.overview::compound",
                    "pathSegment": "contacts-card",
                    "urlSuffix": "/main.js#contacts-card",
                    "dxpOrder": 5,
                    "order": 5,
                    "layoutConfig": {
                      "slot": "content"
                    },
                    "webcomponent": {
                      "selfRegistered": true
                    }
                  },
                  {
                    "entityType": "team.overview::compound",
                    "pathSegment": "contacts-card",
                    "urlSuffix": "/main.js#contacts-card",
                    "dxpOrder": 5,
                    "order": 5,
                    "layoutConfig": {
                      "slot": "content"
                    },
                    "webcomponent": {
                      "selfRegistered": true
                    }
                  },
                  {
                    "entityType": "team.overview::compound",
                    "pathSegment": "links-card",
                    "urlSuffix": "/main.js#links-card",
                    "layoutConfig": {
                      "slot": "content"
                    },
                    "webcomponent": {
                      "selfRegistered": true
                    }
                  },
                  {
                    "entityType": "team.overview::compound",
                    "pathSegment": "docs-card",
                    "urlSuffix": "/main.js#docs-card",
                    "layoutConfig": {
                      "slot": "content"
                    },
                    "webcomponent": {
                      "selfRegistered": true
                    }
                  },
                  {
                    "entityType": "project.boundedContext.overview::compound",
                    "pathSegment": "contacts-card",
                    "urlSuffix": "/main.js#contacts-card",
                    "dxpOrder": 5,
                    "order": 5,
                    "layoutConfig": {
                      "slot": "content"
                    },
                    "webcomponent": {
                      "selfRegistered": true
                    }
                  },
                  {
                    "entityType": "project.apiDefinition.overview::compound",
                    "pathSegment": "contacts-card",
                    "urlSuffix": "/main.js#contacts-card",
                    "dxpOrder": 5,
                    "order": 5,
                    "layoutConfig": {
                      "slot": "content"
                    },
                    "webcomponent": {
                      "selfRegistered": true
                    }
                  },
                  {
                    "entityType": "project.component.overview::compound",
                    "pathSegment": "docs-card",
                    "urlSuffix": "/main.js#docs-card",
                    "dxpOrder": 6,
                    "order": 6,
                    "layoutConfig": {
                      "slot": "content"
                    },
                    "webcomponent": {
                      "selfRegistered": true
                    }
                  },
                  {
                    "entityType": "project.boundedContext.overview::compound",
                    "pathSegment": "docs-card",
                    "urlSuffix": "/main.js#docs-card",
                    "dxpOrder": 6,
                    "order": 6,
                    "layoutConfig": {
                      "slot": "content"
                    },
                    "webcomponent": {
                      "selfRegistered": true
                    }
                  },
                  {
                    "entityType": "project.apiDefinition.overview::compound",
                    "pathSegment": "docs-card",
                    "urlSuffix": "/main.js#docs-card",
                    "dxpOrder": 6,
                    "order": 6,
                    "layoutConfig": {
                      "slot": "content"
                    },
                    "webcomponent": {
                      "selfRegistered": true
                    }
                  },
                  {
                    "entityType": "project.component.overview::compound",
                    "pathSegment": "inner-source-card",
                    "urlSuffix": "/main.js#inner-source-card",
                    "dxpOrder": 7,
                    "order": 7,
                    "layoutConfig": {
                      "slot": "content"
                    },
                    "webcomponent": {
                      "selfRegistered": true
                    }
                  },
                  {
                    "entityType": "project.component.overview::compound",
                    "pathSegment": "dependencies-card",
                    "urlSuffix": "/main.js#related-entities-card",
                    "dxpOrder": 8,
                    "order": 8,
                    "context": {
                      "type": "dependencies"
                    },
                    "layoutConfig": {
                      "slot": "content"
                    },
                    "webcomponent": {
                      "selfRegistered": true
                    }
                  },
                  {
                    "entityType": "project.component.overview::compound",
                    "pathSegment": "exposes-card",
                    "urlSuffix": "/main.js#related-entities-card",
                    "dxpOrder": 9,
                    "order": 9,
                    "context": {
                      "type": "exposes"
                    },
                    "layoutConfig": {
                      "slot": "content"
                    },
                    "webcomponent": {
                      "selfRegistered": true
                    }
                  },
                  {
                    "entityType": "project.overview::compound",
                    "pathSegment": "components-card",
                    "urlSuffix": "/main.js#components-card",
                    "context": {
                      "type": "projectDetail"
                    },
                    "layoutConfig": {
                      "slot": "content",
                      "column": "span 2",
                      "order": 10
                    },
                    "webcomponent": {
                      "selfRegistered": true
                    }
                  },
                  {
                    "entityType": "team.overview::compound",
                    "pathSegment": "components-card",
                    "urlSuffix": "/main.js#components-card",
                    "dxpOrder": 1,
                    "order": 1,
                    "context": {
                      "type": "teamDetail"
                    },
                    "layoutConfig": {
                      "slot": "content",
                      "column": "1 / span 2"
                    },
                    "webcomponent": {
                      "selfRegistered": true
                    }
                  },
                  {
                    "entityType": "project.boundedContext.overview::compound",
                    "pathSegment": "bounded-context-overview-header",
                    "urlSuffix": "/main.js#bounded-context-overview-header",
                    "layoutConfig": {
                      "slot": "header"
                    },
                    "webcomponent": {
                      "selfRegistered": true
                    }
                  },
                  {
                    "entityType": "project.boundedContext.overview::compound",
                    "pathSegment": "components-card",
                    "urlSuffix": "/main.js#components-card",
                    "dxpOrder": 1,
                    "order": 1,
                    "context": {
                      "type": "boundedContextDetail"
                    },
                    "layoutConfig": {
                      "slot": "content",
                      "column": "1 / span 2",
                      "row": "first"
                    },
                    "webcomponent": {
                      "selfRegistered": true
                    }
                  },
                  {
                    "entityType": "project.component.overview::compound",
                    "pathSegment": "components-card",
                    "urlSuffix": "/main.js#components-card",
                    "dxpOrder": 10,
                    "order": 10,
                    "context": {
                      "type": "componentDetail"
                    },
                    "layoutConfig": {
                      "slot": "content",
                      "column": "1 / span 2"
                    },
                    "webcomponent": {
                      "selfRegistered": true
                    }
                  },
                  {
                    "entityType": "project.apiDefinition.overview::compound",
                    "pathSegment": "components-card",
                    "urlSuffix": "/main.js#components-card",
                    "context": {
                      "type": "apiDefinitionDetail"
                    },
                    "layoutConfig": {
                      "slot": "content",
                      "column": "1 / span 2",
                      "row": "first"
                    },
                    "webcomponent": {
                      "selfRegistered": true
                    }
                  },
                  {
                    "entityType": "project.apiDefinition.overview::compound",
                    "pathSegment": "api-definition-overview-header",
                    "urlSuffix": "/main.js#api-definition-overview-header",
                    "layoutConfig": {
                      "slot": "header"
                    },
                    "webcomponent": {
                      "selfRegistered": true
                    }
                  }
                ],
                  "texts": [
                    {
                      "locale": "",
                      "textDictionary": {}
                    }
                  ]
              }
          }
      }`
}

func GetValidJSON_organization_ui2() string {
	return `{
        "name": "organization-ui",
        "luigiConfigFragment": {
          "data": {
            "viewGroup": {
              "preloadSuffix": "/#/preload",
              "requiredIFramePermissions": {
                "allow": ["clipboard-read", "clipboard-write"]
              }
            },
            "nodes": [
              {
                "entityType": "global",
                "pathSegment": "products",
                "label": "{{products}}",
                "hideSideNav": true,
                "icon": "product",
                "urlSuffix": "/{i18n.currentLocale}/#/products",
                "dxpOrder": 2,
                "order": 2,
                "navigationContext": "projects",
                "visibleForFeatureToggles": ["splitProjectByType"]
              },
              {
                "entityType": "global",
                "pathSegment": "experiments",
                "label": "{{experiments}}",
                "hideSideNav": true,
                "icon": "lab",
                "urlSuffix": "/{i18n.currentLocale}/#/experiments",
                "dxpOrder": 2.1,
                "order": 2.1,
                "navigationContext": "projects",
                "visibleForFeatureToggles": ["splitProjectByType"]
              },
              {
                "entityType": "global",
                "pathSegment": "projects",
                "label": "{{projects}}",
                "hideSideNav": true,
                "icon": "curriculum",
                "urlSuffix": "/{i18n.currentLocale}/#/projects",
                "dxpOrder": 2,
                "order": 2,
                "navigationContext": "projects",
                "visibleForFeatureToggles": ["!splitProjectByType"],
                "context": {
                  "_tmpUseBreadcrumbsForTitle": true
                },
                "children": [
                  {
                    "pathSegment": ":projectId",
                    "hideFromNav": true,
                    "navHeader": {
                      "useTitleResolver": true
                    },
                    "titleResolver": {
                      "request": {
                        "method": "GET",
                        "url": "${frameContext.accountSearchServiceApiUrl}?q=name:${projectId}",
                        "headers": {
                          "authorization": "Bearer ${token}"
                        }
                      },
                      "titlePropertyChain": "docs[0].displayName",
                      "prerenderFallback": false,
                      "fallbackTitle": "{{project}}",
                      "fallbackIcon": "curriculum"
                    },
                    "defineEntity": {
                      "id": "project",
                      "contextKey": "projectId",
                      "dynamicFetchId": "project",
                      "useBack": true,
                      "label": "{{project}}",
                      "pluralLabel": "{{projects}}",
                      "notFoundConfig": {
                        "entityListNavigationContext": "projects",
                        "sapIllusSVG": "Scene-NoSearchResults"
                      }
                    },
                    "context": {
                      "projectId": ":projectId"
                    },
                    "navigationContext": "project",
                    "children": [
                      {
                        "defineSlot": "main"
                      },
                      {
                        "defineSlot": ""
                      },
                      {
                        "defineSlot": "devopsMetrics",
                        "category": {
                          "label": "{{devopsMetrics}}",
                          "collapsible": false
                        }
                      },
                      {
                        "defineSlot": ""
                      },
                      {
                        "defineSlot": "settings",
                        "category": {
                          "label": "{{settings}}",
                          "collapsible": false
                        }
                      }
                    ]
                  }
                ]
              },
              {
                "entityType": "global",
                "pathSegment": "projects",
                "label": "{{projects}}",
                "hideFromNav": true,
                "hideSideNav": true,
                "urlSuffix": "/{i18n.currentLocale}/#/products",
                "navigationContext": "projects",
                "visibleForFeatureToggles": ["splitProjectByType"],
                "children": [
                  {
                    "pathSegment": ":projectId",
                    "hideFromNav": true,
                    "navHeader": {
                      "useTitleResolver": true
                    },
                    "titleResolver": {
                      "request": {
                        "method": "GET",
                        "url": "${frameContext.accountSearchServiceApiUrl}?q=name:${projectId}",
                        "headers": {
                          "authorization": "Bearer ${token}"
                        }
                      },
                      "titlePropertyChain": "docs[0].displayName",
                      "prerenderFallback": false,
                      "fallbackTitle": "{{project}}",
                      "fallbackIcon": "curriculum"
                    },
                    "defineEntity": {
                      "id": "project",
                      "contextKey": "projectId",
                      "dynamicFetchId": "project",
                      "useBack": true,
                      "label": "{{project}}",
                      "pluralLabel": "{{projects}}",
                      "notFoundConfig": {
                        "entityListNavigationContext": "projects",
                        "sapIllusSVG": "Scene-NoSearchResults"
                      }
                    },
                    "context": {
                      "projectId": ":projectId"
                    },
                    "navigationContext": "project",
                    "children": [
                      {
                        "defineSlot": "main"
                      },
                      {
                        "defineSlot": ""
                      },
                      {
                        "defineSlot": "devopsMetrics",
                        "category": {
                          "label": "{{devopsMetrics}}",
                          "collapsible": false
                        }
                      },
                      {
                        "defineSlot": ""
                      },
                      {
                        "defineSlot": "settings",
                        "category": {
                          "label": "{{settings}}",
                          "isGroup": true
                        }
                      }
                    ]
                  }
                ]
              },
              {
                "entityType": "global",
                "pathSegment": "teams",
                "label": "{{teams}}",
                "urlSuffix": "/{i18n.currentLocale}/#/teams",
                "hideSideNav": true,
                "icon": "group",
                "dxpOrder": 3,
                "order": 3,
                "navigationContext": "teams",
                "children": [
                  {
                    "pathSegment": ":teamId",
                    "hideFromNav": true,
                    "navHeader": {
                      "useTitleResolver": true
                    },
                    "titleResolver": {
                      "request": {
                        "method": "GET",
                        "url": "${frameContext.accountSearchServiceApiUrl}?q=name:${teamId}&fuzzy=false&fq=accountRole%3A%22Team%22",
                        "headers": {
                          "authorization": "Bearer ${token}"
                        }
                      },
                      "titlePropertyChain": "docs[0].displayName",
                      "prerenderFallback": false,
                      "fallbackTitle": "{{team}}",
                      "fallbackIcon": "group"
                    },
                    "defineEntity": {
                      "id": "team",
                      "contextKey": "teamId",
                      "dynamicFetchId": "team",
                      "useBack": true,
                      "label": "{{team}}",
                      "pluralLabel": "{{teams}}",
                      "notFoundConfig": {
                        "entityListNavigationContext": "teams",
                        "sapIllusSVG": "Scene-NoSearchResults"
                      }
                    },
                    "context": {
                      "teamId": ":teamId"
                    },
                    "navigationContext": "team",
                    "children": [
                      {
                        "defineSlot": "main"
                      },
                      {
                        "defineSlot": ""
                      },
                      {
                        "defineSlot": "settings",
                        "category": {
                          "label": "{{settings}}",
                          "collapsible": false
                        }
                      }
                    ]
                  }
                ]
              },
              {
                "pathSegment": "projects-create-dialog",
                "entityType": "global",
                "hideFromNav": true,
                "hideSideNav": true,
                "navigationContext": "projects",
                "context": {
                  "_tmpUseBreadcrumbsForTitle": true
                },
                "urlSuffix": "/{i18n.currentLocale}/#/projects-create-dialog"
              },
              {
                "pathSegment": "create-product",
                "entityType": "global",
                "hideFromNav": true,
                "hideSideNav": true,
                "label": "{{createProduct}}",
                "visibleForFeatureToggles": ["splitProjectByType"],
                "urlSuffix": "/{i18n.currentLocale}/#/create-project?type=product"
              },
              {
                "pathSegment": "create-experiment",
                "entityType": "global",
                "hideFromNav": true,
                "hideSideNav": true,
                "label": "{{createExperiment}}",
                "visibleForFeatureToggles": ["splitProjectByType"],
                "urlSuffix": "/{i18n.currentLocale}/#/create-project?type=experiment"
              },
              {
                "pathSegment": "create-project",
                "entityType": "global",
                "hideFromNav": true,
                "hideSideNav": true,
                "context": {
                  "_tmpUseBreadcrumbsForTitle": true
                },
                "urlSuffix": "/{i18n.currentLocale}/#/choose-project-type"
              },
              {
                "pathSegment": "create-project-details",
                "entityType": "global",
                "hideFromNav": true,
                "hideSideNav": true,
                "context": {
                  "_tmpUseBreadcrumbsForTitle": true
                },
                "urlSuffix": "/{i18n.currentLocale}/#/create-project"
              },
              {
                "pathSegment": "edit-project",
                "entityType": "project",
                "hideFromNav": true,
                "hideSideNav": true,
                "label": "Edit Project",
                "context": {
                  "_tmpUseBreadcrumbsForTitle": true
                },
                "urlSuffix": "/{i18n.currentLocale}/#/edit-project"
              },
              {
                "pathSegment": "edit-team",
                "entityType": "team",
                "hideFromNav": true,
                "hideSideNav": true,
                "label": "Edit Team",
                "context": {
                  "_tmpUseBreadcrumbsForTitle": true
                },
                "urlSuffix": "/{i18n.currentLocale}/#/edit-team"
              },
              {
                "pathSegment": "create-team",
                "entityType": "global",
                "hideFromNav": true,
                "hideSideNav": true,
                "label": "{{createTeam}}",
                "context": {
                  "_tmpUseBreadcrumbsForTitle": true
                },
                "urlSuffix": "/{i18n.currentLocale}/#/create-team"
              }
            ],
            "texts": [
              {
                "locale": "",
                "textDictionary": {
                  "projects": "Projects",
                  "project": "Project",
                  "products": "Products",
                  "experiments": "Experiments",
                  "createProduct": "Create Product",
                  "createExperiment": "Create Experiment",
                  "createTeam": "Create Team",
                  "teams": "Teams",
                  "team": "Team",
                  "settings": "Settings & Accounts",
                  "devopsMetrics": "DevOps Metrics"
                }
              },
              {
                "locale": "en",
                "textDictionary": {
                  "projects": "Projects",
                  "project": "Project",
                  "products": "Products",
                  "experiments": "Experiments",
                  "createProduct": "Create Product",
                  "createExperiment": "Create Experiment",
                  "createTeam": "Create Team",
                  "teams": "Teams",
                  "team": "Team",
                  "settings": "Settings & Accounts",
                  "devopsMetrics": "DevOps Metrics"
                }
              },
              {
                "locale": "de",
                "textDictionary": {
                  "projects": "Projekte",
                  "project": "Projekt",
                  "products": "Produkte",
                  "experiments": "Experimente",
                  "createProduct": "Produkt erstellen",
                  "createExperiment": "Experiment erstellen",
                  "createTeam": "Team erstellen",
                  "teams": "Teams",
                  "team": "Team",
                  "settings": "Einstellungen & Accounts",
                  "devopsMetrics": "DevOps Metrics"
                }
              }
            ]
          }
        }
      }`
}

func GetValidJSON_review_extension() string {
	return `{
        "name": "review-extension",
        "luigiConfigFragment": {
              "data": {
                  "viewGroup": {
                    "preloadSuffix": "/#/preload",
                    "requiredIFramePermissions": {
                      "allow": ["clipboard-read", "clipboard-write"]
                    }
                  },
                  "nodes": [
                    {
                      "pathSegment": "github-review",
                      "icon": "{{extensionIcon}}",
                      "label": "{{nodeName}}",
                      "category": {
                        "id": "community-extensions",
                        "dxpOrder": 30,
                        "order": 30
                      },
                      "navHeader": {
                        "label": "{{nodeName}}",
                        "icon": "{{extensionIcon}}"
                      },
                      "entityType": "project",
                      "visibleForEntityContext": {
                        "project": {
                          "policies": ["iamMember"]
                        }
                      },
                      "children": [
                        {
                          "pathSegment": "review-open-developer-pull-requests",
                          "label": "Open PRs by Developers",
                          "urlSuffix": "/{i18n.currentLocale}/#/review-pull-requests?q=-author:app/ospo-renovate%20draft:false%20is:open%20archived:false",
                          "hideSideNav": false,
                          "isolateView": true,
                          "entityType": "project",
                          "context": {
                            "title": "Open Review Pull Requests",
                            "subTitle": "This page lists open pull requests for your Project. A pull request will be listed here if your repo is onboarded to the Project. We also filter out Pull requests created by ospo-renovate, draft PRs and archived Repo's."
                          },
                          "category": {
                            "label": "{{pullRequests}}",
                            "collapsable": false,
                            "dxpOrder": 100,
                            "order": 100
                          }
                        },
                        {
                          "pathSegment": "review-open-renovate-pull-requests",
                          "label": "Open PRs By Renovate",
                          "urlSuffix": "/{i18n.currentLocale}/#/review-pull-requests?q=author:app/ospo-renovate%20draft:false%20is:open%20archived:false",
                          "hideSideNav": false,
                          "isolateView": true,
                          "entityType": "project",
                          "context": {
                            "title": "Open Renovate Pull Requests",
                            "subTitle": "This page lists open pull requests by ospo-renovate for your Project. A pull request will be listed here if your repo is onboarded to the Project. We also filter out draft PRs and archived Repo's."
                          },
                          "category": {
                            "label": "{{pullRequests}}",
                            "collapsable": false,
                            "dxpOrder": 100,
                            "order": 100
                          },
                          "clientPermissions": {
                            "urlParameters": {
                              "author": {
                                "read": true,
                                "write": true
                              }
                            }
                          }
                        },
                        {
                          "pathSegment": "review-all-developer-pull-requests",
                          "label": "All PRs by Developers",
                          "urlSuffix": "/{i18n.currentLocale}/#/review-pull-requests?q=-author:app/ospo-renovate%20archived:false",
                          "hideSideNav": false,
                          "isolateView": true,
                          "entityType": "project",
                          "context": {
                            "title": "Review Pull Requests",
                            "subTitle": "This page lists all pull requests for your Project. A pull request will be listed here if your repo is onboarded to the Project. We filter out Pull requests created by ospo-renovate, draft PRs and archived Repo's."
                          },
                          "category": {
                            "label": "{{pullRequests}}",
                            "collapsable": false,
                            "dxpOrder": 100,
                            "order": 100
                          }
                        },
                        {
                          "pathSegment": "review-pull-requests-involved",
                          "label": "Involved",
                          "urlSuffix": "/{i18n.currentLocale}/#/review-pull-requests?q=involves:{context.userid}%20draft:false%20is:open%20archived:false",
                          "hideSideNav": false,
                          "isolateView": true,
                          "entityType": "project",
                          "context": {
                            "title": "Pull Requests with your involvement",
                            "subTitle": "This page lists all open pull requests you are involved in in any way. You are either author, assignee, mentioned, or commenter. A pull request will be listed here if your repo is onboarded to the Project. We also filter out draft PRs and archived Repo's."
                          },
                          "category": {
                            "label": "{{myPullRequests}}",
                            "collapsable": false,
                            "dxpOrder": 100,
                            "order": 100
                          },
                          "clientPermissions": {
                            "urlParameters": {
                              "author": {
                                "read": true,
                                "write": true
                              }
                            }
                          }
                        },
                        {
                          "pathSegment": "review-pull-requests-authored-by-me",
                          "label": "Authored",
                          "urlSuffix": "/{i18n.currentLocale}/#/review-pull-requests?q=author:{context.userid}%20draft:false%20is:open%20archived:false",
                          "hideSideNav": false,
                          "isolateView": true,
                          "entityType": "project",
                          "context": {
                            "title": "Pull Requests you authored",
                            "subTitle": "This page lists all open pull requests authored by you. A pull request will be listed here if your repo is onboarded to the Project. We also filter out draft PRs and archived Repo's."
                          },
                          "category": {
                            "label": "{{myPullRequests}}",
                            "collapsable": false,
                            "dxpOrder": 100,
                            "order": 100
                          },
                          "clientPermissions": {
                            "urlParameters": {
                              "author": {
                                "read": true,
                                "write": true
                              }
                            }
                          }
                        },
                        {
                          "pathSegment": "review-pull-requests-reviews-requested",
                          "label": "Review Requested",
                          "urlSuffix": "/{i18n.currentLocale}/#/review-pull-requests?q=review-requested:{context.userid}%20draft:false%20is:open%20archived:false",
                          "hideSideNav": false,
                          "isolateView": true,
                          "entityType": "project",
                          "context": {
                            "title": "Pull Requests that request your review",
                            "subTitle": "This page lists all open pull requests that request your review. A pull request will be listed here if your repo is onboarded to the Project. We also filter out draft PRs, closed PRs and archived Repo's."
                          },
                          "category": {
                            "label": "{{myPullRequests}}",
                            "collapsable": false,
                            "dxpOrder": 100,
                            "order": 100
                          },
                          "clientPermissions": {
                            "urlParameters": {
                              "author": {
                                "read": true,
                                "write": true
                              }
                            }
                          }
                        },
                        {
                          "pathSegment": "review-pull-requests-that-mention-me",
                          "label": "Mentioned",
                          "urlSuffix": "/{i18n.currentLocale}/#/review-pull-requests?q=mentions:{context.userid}%20draft:false%20is:open%20archived:false",
                          "hideSideNav": false,
                          "isolateView": true,
                          "entityType": "project",
                          "context": {
                            "title": "Pull Requests that mention you",
                            "subTitle": "This page lists all pull open requests that mention you. A pull request will be listed here if your repo is onboarded to the Project. We also filter out draft PRs, closed PRs and archived Repo's."
                          },
                          "category": {
                            "label": "{{myPullRequests}}",
                            "collapsable": false,
                            "dxpOrder": 100,
                            "order": 100
                          },
                          "clientPermissions": {
                            "urlParameters": {
                              "author": {
                                "read": true,
                                "write": true
                              }
                            }
                          }
                        },
                        {
                          "pathSegment": "review-pull-requests-reviewed-by-me",
                          "label": "Reviewed",
                          "urlSuffix": "/{i18n.currentLocale}/#/review-pull-requests?q=reviewed-by:{context.userid}%20draft:false%20archived:false",
                          "hideSideNav": false,
                          "isolateView": true,
                          "entityType": "project",
                          "context": {
                            "title": "Pull Requests Review by you",
                            "subTitle": "This page lists pull requests that have been reviewed by you. A pull request will be listed here if your repo is onboarded to the Project. We also filter out draft PRs and archived Repo's."
                          },
                          "category": {
                            "label": "{{myPullRequests}}",
                            "collapsable": false,
                            "dxpOrder": 100,
                            "order": 100
                          },
                          "clientPermissions": {
                            "urlParameters": {
                              "author": {
                                "read": true,
                                "write": true
                              }
                            }
                          }
                        }
                      ]
                    }
                  ],
                  "texts": [
                    {
                      "locale": "",
                      "textDictionary": {
                        "pullRequests": "Project Pull Requests",
                        "myPullRequests": "My Pull Requests",
                        "nodeName": "GitHub Review",
                        "renovateIcon": "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAADIAAAAyCAYAAAAeP4ixAAAAAXNSR0IArs4c6QAAAIRlWElmTU0AKgAAAAgABQESAAMAAAABAAEAAAEaAAUAAAABAAAASgEbAAUAAAABAAAAUgEoAAMAAAABAAIAAIdpAAQAAAABAAAAWgAAAAAAAABIAAAAAQAAAEgAAAABAAOgAQADAAAAAQABAACgAgAEAAAAAQAAADKgAwAEAAAAAQAAADIAAAAAhvHCqAAAAAlwSFlzAAALEwAACxMBAJqcGAAAAVlpVFh0WE1MOmNvbS5hZG9iZS54bXAAAAAAADx4OnhtcG1ldGEgeG1sbnM6eD0iYWRvYmU6bnM6bWV0YS8iIHg6eG1wdGs9IlhNUCBDb3JlIDYuMC4wIj4KICAgPHJkZjpSREYgeG1sbnM6cmRmPSJodHRwOi8vd3d3LnczLm9yZy8xOTk5LzAyLzIyLXJkZi1zeW50YXgtbnMjIj4KICAgICAgPHJkZjpEZXNjcmlwdGlvbiByZGY6YWJvdXQ9IiIKICAgICAgICAgICAgeG1sbnM6dGlmZj0iaHR0cDovL25zLmFkb2JlLmNvbS90aWZmLzEuMC8iPgogICAgICAgICA8dGlmZjpPcmllbnRhdGlvbj4xPC90aWZmOk9yaWVudGF0aW9uPgogICAgICA8L3JkZjpEZXNjcmlwdGlvbj4KICAgPC9yZGY6UkRGPgo8L3g6eG1wbWV0YT4KGV7hBwAAEA9JREFUaAXtWXtwVNUZ//befWTz2E3IA0ISyIYQHgIqoGCBmrR0VARqmaJS66hMxwft6NiiResjQhUdptXBFiraQodaC9Si0hGs8ijPqYCWRwR5JhGSACHZPDf7vP39zu5dA1mX+JiOf/jNnL33nvOd73zv75yzIt/A10sDlq+YHUWvsrIyIV30G7H1zOdXvPyXI2cpLy+3bt682WoYhnYpUsQhLucAN6HAl6LxlY/ffffdNjKVgLCOvjS0nFjjew8hOZc0MPal4YtqRFu8eLHt/vvv98c4sON51aOPPvptj8cz2u12e9LT090OhyOD436/v621tdXb1tZ2ErD3mWee2Ybu3WhBjoOWA7T4HuH3/wXGjBljg3uYCsi57777frl69eq9AAOMYig5QCBjz549xqpVq/bce++9D4PpPmQcs5SLflEhTIYSzkdwaufPn1emHzFiROSNN97Q1q9fr6wwbdq0ObNnz543adKkouzsbHM+xxjIpGs2jrHPbOx3sBO0ZevWrTVLly5d+O67777EvpkzZzovu+yyMMYsDQ0Nxpo1awLs/8JAIT5jcsaCBQvW1dTUmKoP4qULLWR29OJJXM7hXOPEiRMG1luL9VI/Y82kCuechAgUAo3+mvfAAw/ckQXQNK0OvrzzqaeeegUBeqXVauV4GE1ln0gkIp2dndLR0cGYUC0c5rCIruuCeFEtNTVV2NgHoJUYG9ZgMKi99NJLu95///2Fo0aNGtHe3u6Csg6sWLHir0QEkFfiJ4SEgsQwteXLl79/yy23jHE6nfLJJ5/IunXr5K677hJ8h6FIC0Dr6uqSc+fOSVNTk/Ad/YJ+RcJ8so9gjlEo6Eby8vJIi/0R4BpgXl+7dq1cf/31kpubKxBEINzjCxcu/DWetnvuuUclB0Xsop8e7tPNpUqHDh1KIajWIAQJT506lQtHsLBOIerq6+XAgQNy6tRpCYYiYrXZBZYSm82mGrXOxm+rNfq02VMkFI7I6dOn5eDBg5h7iixppIlMF0HMhZHZQujzDxw4UAYPHvx9IsALoublRwLoIUhVVZVpJQ3uQvfRjx07ZoOb6AMGDCAJNef48eNy8sQJMGiT9PQ0sUQColkMMO0QTqP2TeC3xaIrQY1Ql+jAy3C5Be4qtbW1AvpqDmkXFxfrsIwVAqokAyWo5LKF6/52p/MGpOrVq2cqvzTp85momJnjBogooWANY+TIkaaAUldXJw0N9ZKR4Zb2Nq9UHTwgdc0BcTstMnxIqeTlF0ooGFQuRoFolWAwIFX790rN2Q6x6xEZNqhICgeWKteia9rtdqGi6I7Dhw+XI0eOGIWFhfiOxnGFxUIrhdbjh23zZrEWVxdbi2uqA5ZKiSQTxEKXoN9To2aKDQQCEKJBnKnp4utslw0bt8uC1sGo4QORT1tl1ts75c4pIn37F0kAQU+tU5gd27fJ/No88eaNhNMEpOKdXXL/5Ih4SocqIRobG+Mx069fPzl06JCEQiFaWHnAra/V9l9z5vyEmY69x1za0oMVFXsRL9UUToxK0ZIJQhxBAYPrpCtN8ZuZiQs4nXY5Xn1c/tQMrZWNkYJwl3jdfeQ1TZcrD+6W7/UrgAAwOeKm4dQJeasmRbyDvyX5RhB9Dtmsl8uYfZuloKhIdCviJhRUtBn8BJcrQ7zeFuDalCA7GsPTh7pdS0OBxq5BjstPrlrp/niso25biRxeYblZmnrEiKLS7YcWSEvjVikKXT5fNAcaEWnv7JLTablSgCzs8/vEGewSSc+U5o6IhIN+scAadJX2Vq80peap6AoEYGF/J3ZiGdIYSJFgIIpHq9P6JmRkuASxIrqmXEp8ujYh2wqlal57vtsYVpQTvqkks+E3fpuM5ZykgtAluEAs56s1QqwNVLVFk5w+bhnbclJOIWNFHGlyTkfBbvxECrNTxOZAWo2EMT8s2bn5UtJxUrlUm90pLQ7UvcZ6KXEFxJGSBjzmFGy0Yk++UwGcq2kWlXJzD20MeD94b1Hdvv1zz7f47mk+1Xqkxes91dHV5xDxk7kWuBUVJz5YwQQGJSEUDkn/Io/cOaRejCObZF/2YCnqaJJZ9o9k1KhyxRSYUEGfmdNXZozOlva978imPiMkK9gps/z75JpJV4kONwshEZBxpm4TuCZriabp3KJcbX9xtrYPlX+/SP2/RA6IZDYu+klu1tgrpZlzPp1pUuj2BPEIfFZDnMQzllmVqW2UEhl/zQQpzD8qjU1HJDXFLsWDJoszLT2WtaKBHkZMjbxirDyYfVJmNRwTu80qA4oniisrOy4EkwJpm+D1ejWPxyNwbabha42rylvcZ+sLU7TW77hscjMY2vPQK7IXcrVXlkMH5sQETx2upWVmZvq51UCQ27CQxnhxuVwIRC8yStQzPWUjxIOYobuR6SDiioxRy1EXiagEUTBwMFLuIIVHtyEe3ZbJgwmFjYC1wnCzQEZGhhPbHUrXmCqtNdaaj/f7MzLLmuwpBbolOGHCMN+ZHXCsyi1YTs3s9oPdZtRhRWqPHj16HkMO5HMHKjFxw2SM+Z6MUkB++7t80FxQPcPIPmSO/dg/qWYKFUBCYPLw+7viQpj7sSJmL8zjGqjsOqq6Sl94r0Lf8pChae6SkhxfwFfV1tbUgBJbt+NY2gecUI7w6lEhOVCJTeOWLVv82A/958yZMzkoTgchSJ/Ro0fzoBRCfdGgLWUVMqYYx4CZpcgchejbt6/KeC0tLSQbF5BCEojDuaWlpYLDmOrCj/XNN988jTqydceOHVsefvjhn6MvgCLc2tzcPA485cFKh2Gx/cEuL8IG1QSCxH2fHd0BboX1sJf4FCZu2LBh23XXXcceBqCd6ZJVnm5G92CWo/ZTUlJUceOmkEyfPXtWNQYwMxP7GNh00f79+5uxwdybgvOOMWXKlGvw/h80ee655/ojDS/FnP7YAdTj7PJbdG/hGID8d+dRdfb4oTCME5wAVaoaN27c7Tt37kSXAu6A4wChDPi2AQvF+y5+gQUUDnEhUPdhdZbBIcvApvFWMoI1lWs9/fTTT77wwgsGjsc8sxi33XbbVRzHezR98gPQI9i5Xa6vr1eWwtlD8vPzKbGxaNGitIceemglz9boX1ZRUZEB7ZMheJSmLMEnuOuxnWcfrcBxM8XSJWPWIQ1906ZN7U888cRdH3744d/BQyrOJYoHzNNYGImP+Q3jx48//eqrr5J3M5b5fqEgkFJLsucP8sZj2bJlf4O2Cnfv3r0IGuZex0aGyKzJMJmm75NxvjNm2C7GwZjyX+IiHmZAiHfJFHhA6Y8CcDopPF0XYEe8MR33ANMiytcgSGTJkiVZmJwKPw9DC0orKILIGZoOlzhLCsXFxdcCl69qnC9fENS6kydPtuAA59q4caOAbhloge+QDgHOQwH9TIVAEQb6EsaEEgSTdbQQXOYWBNQSCMJDDE3HhdgY3H1A9N+wyFzcgnyXWhwyZIjOYy0XMq3Byo8zjQpg+LOyzHvvvafOHMOGDVOWibmUshbjCkxbqqurpyMe0mDlP4NWExjWiYexFKwdxHo8ExlMMInACgFYH5S/gcANSKt9cK1zAXMkSPNike/hduNubOOddBWcUyzENYFZiLvlefPmyY033qiyF8fuuOMOnB82C+69lIBg3pzCp8ZjAc7pV6DglnL3i+zGOwLo06IUBBwfeKNLaTgm96h9igh/nnzySWUuIPsZVAAfGI2QebPxbAKtBLHIOJ6lARdohzWDQjz22GNCS6D2MHXKs88+K4gn4TH5+eefV1t1uCbnK4uALi3Cq6EiMJ9JpaHPwnW5Js/3eHdyRwH+IvCAC7SgCOHHjBH1TRUA+G7H0w8LvIF35lMLtwqAnagJt8MNiGNgXCGnpjoFhVOuGDlcbvr+dNm+fbvg4o44Cl5//XXBFZJMnzZVpk+dIm/9820ZgEpeiwsN0KYSLahFWVDUYijSwHMg+jrAA8/yjE8qLRUCrXv88ccZpxZ40gVn+AsEwRzFGBBZ8VuA/CM84wDfT0carmSlpsaoSYI7Mwua9sn4Sd+RrmBYXl6m7tqkrKxMePKjlsGATJh0rYyfWK4EgYpjdJXmInA3DVdJ4fnz5y+IDSR79Aj4CwSBBrpLaeAaJuuRRx5phnu44fctqOw3Iae7uAJwTaGlzat20nK8aq/85Y8tUn3iuGKC7mVCjsshry3/gzTidpFwtr5OPUGFdMKwNAWZpDrhEQ8++KCObUucH9484rjNoqh8P4YXfyhBTJ5gEQcazR1CHzJwipIcbqMmI7AvB0FOZllWkqDUSa7Th9NiqlQU7pKpo3bJH1bYJA2UnTxndVgkw25IY5shPxv9V9mKA8VacUtBeos0YwtmqoPXQ4g/D9wyY+LEiW24ikp6jxWXIPbCG0VKqTaPYL6e6RPbaQpYB614yTCCVOU8aOxKVH1ORbfphqjkISq1U042uqSspFgW/jgkHfA6FzbgmdBFG9Qw/0cBGeIZIEfP5QK3RcKxv1LodgTu16Cogo8++mgQvxGTCbMTxxIBkal1RQ2pbyGI3YB2M/qmcMKLL77IPY2BW8YcLDbk8OHD7FaW4g81Wt9iSEGmyMvvtMr2/dXy0+mGVM4SOXEODEIFT9wq8sAPRHZV1crit85JWT+RQ3VRAbDlirspAtqJzFfCBXCE4KPXoFwLFom6isXCorAh0WxcpJVBwPzYGDeT6jX2wIVDdGTyr7A1fVbkFz8UmX19tC8T56X/ImwqHol+n4mGlAoOWoSnULhVGIri5dwIYP0DCmR80NTRhaJTP/NXCcJRuJVSsOlm6IpQQFMzOAtcDdeiC6qLOzNjcS6hE1cEmajBtED5PJE5sOe1IzEAVhgXv39boUkW9rTNuAKgJU0lsPhhNAxr6MhepVFMCWF95fqx76SPuCAxLMZLNKeiI0YILKrj50gIw9cwUq+OGsL3OJATCoHkJLk4Iy0B42wmDMpD4LdGhSDb8Kg4xKxrsBZBkMtwKkzxeDygpm55oj4Yx078cqmA4jjdjqlxOBfiN6xHvi8AZU70tvpFjqNkDYUTjvagFUff2dcC1jizuxAkwu0OgPstHgGKtm3bBrGFR4ge67A/EVxskYtxlKArV64sRHyUxNzMcrFbmZPoKlyZQh1Wyc0ciT67u1P3EdKDe/HSnBbJRawMxHgt/s7rjpb0PalFcIwlTzymDoH/Ki1hPR5yaO6EDRMS9hMfgiYc474OeynqIICgZxoejHeBKxO/V5DUIjhPm6bVzKsaaC3hwaZXqyVBAvMc1XmpAVAC4Ehhrs++pJBUEOytlPPOnTv3HeyVfod/YW9HkPvoBp8WxKT0ezXIjAkFcbftxDZkPa6G1nDinDlzDNzm9I7GpbBimUtpCGeKTLiYA1U3YlroUvN7M84zOYSg9sMzZsxQe6Du6/aGRq9wSBR/G6ttTK8mfDkk1o6ksZuIfK99EJMtuN1I6oqJFvi8fbjg4IZVJZnPO/cb/K+TBv4HlpK+riAzQXYAAAAASUVORK5CYII=",
                        "extensionIcon": "data:image/png;base64,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"
                      }
                    },
                    {
                      "locale": "en",
                      "textDictionary": {
                        "pullRequests": "Project Pull Requests",
                        "myPullRequests": "My Pull Requests",
                        "nodeName": "GitHub Review",
                        "renovateIcon": "data:image/png;base64,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",
                        "extensionIcon": "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAGQAAABkCAYAAABw4pVUAAAABGdBTUEAALGPC/xhBQAAACBjSFJNAAB6JgAAgIQAAPoAAACA6AAAdTAAAOpgAAA6mAAAF3CculE8AAAAtGVYSWZNTQAqAAAACAAFARIAAwAAAAEAAQAAARoABQAAAAEAAABKARsABQAAAAEAAABSASgAAwAAAAEAAgAAh2kABAAAAAEAAABaAAAAAAAAAGAAAAABAAAAYAAAAAEAB5AAAAcAAAAEMDIyMZEBAAcAAAAEAQIDAKAAAAcAAAAEMDEwMKABAAMAAAABAAEAAKACAAQAAAABAAAAZKADAAQAAAABAAAAZKQGAAMAAAABAAAAAAAAAADILEW/AAAACXBIWXMAAA7EAAAOxAGVKw4bAAAEemlUWHRYTUw6Y29tLmFkb2JlLnhtcAAAAAAAPHg6eG1wbWV0YSB4bWxuczp4PSJhZG9iZTpuczptZXRhLyIgeDp4bXB0az0iWE1QIENvcmUgNi4wLjAiPgogICA8cmRmOlJERiB4bWxuczpyZGY9Imh0dHA6Ly93d3cudzMub3JnLzE5OTkvMDIvMjItcmRmLXN5bnRheC1ucyMiPgogICAgICA8cmRmOkRlc2NyaXB0aW9uIHJkZjphYm91dD0iIgogICAgICAgICAgICB4bWxuczpleGlmPSJodHRwOi8vbnMuYWRvYmUuY29tL2V4aWYvMS4wLyIKICAgICAgICAgICAgeG1sbnM6dGlmZj0iaHR0cDovL25zLmFkb2JlLmNvbS90aWZmLzEuMC8iPgogICAgICAgICA8ZXhpZjpDb2xvclNwYWNlPjE8L2V4aWY6Q29sb3JTcGFjZT4KICAgICAgICAgPGV4aWY6UGl4ZWxYRGltZW5zaW9uPjEwMjQ8L2V4aWY6UGl4ZWxYRGltZW5zaW9uPgogICAgICAgICA8ZXhpZjpTY2VuZUNhcHR1cmVUeXBlPjA8L2V4aWY6U2NlbmVDYXB0dXJlVHlwZT4KICAgICAgICAgPGV4aWY6RXhpZlZlcnNpb24+MDIyMTwvZXhpZjpFeGlmVmVyc2lvbj4KICAgICAgICAgPGV4aWY6Rmxhc2hQaXhWZXJzaW9uPjAxMDA8L2V4aWY6Rmxhc2hQaXhWZXJzaW9uPgogICAgICAgICA8ZXhpZjpQaXhlbFlEaW1lbnNpb24+MTAyNDwvZXhpZjpQaXhlbFlEaW1lbnNpb24+CiAgICAgICAgIDxleGlmOkNvbXBvbmVudHNDb25maWd1cmF0aW9uPgogICAgICAgICAgICA8cmRmOlNlcT4KICAgICAgICAgICAgICAgPHJkZjpsaT4xPC9yZGY6bGk+CiAgICAgICAgICAgICAgIDxyZGY6bGk+MjwvcmRmOmxpPgogICAgICAgICAgICAgICA8cmRmOmxpPjM8L3JkZjpsaT4KICAgICAgICAgICAgICAgPHJkZjpsaT4wPC9yZGY6bGk+CiAgICAgICAgICAgIDwvcmRmOlNlcT4KICAgICAgICAgPC9leGlmOkNvbXBvbmVudHNDb25maWd1cmF0aW9uPgogICAgICAgICA8dGlmZjpSZXNvbHV0aW9uVW5pdD4yPC90aWZmOlJlc29sdXRpb25Vbml0PgogICAgICAgICA8dGlmZjpPcmllbnRhdGlvbj4xPC90aWZmOk9yaWVudGF0aW9uPgogICAgICAgICA8dGlmZjpYUmVzb2x1dGlvbj45NjwvdGlmZjpYUmVzb2x1dGlvbj4KICAgICAgICAgPHRpZmY6WVJlc29sdXRpb24+OTY8L3RpZmY6WVJlc29sdXRpb24+CiAgICAgIDwvcmRmOkRlc2NyaXB0aW9uPgogICA8L3JkZjpSREY+CjwveDp4bXBtZXRhPgqoFo6OAABAAElEQVR4AcR9B2BUxfb32d1sem8kkEAaCRAglNBbKNJ7EWmiojwrimDXJ4gCoqJPpAqiFBWpUgXpJbSEFtJJJb33srvZ+X5nbjYF8fn8P33fwN29O3faPWfm9Jmo6P9zmjZtmiYmJkYTHR1dh6Hw1SwtWbLENvpGtEd5VblXbU1t6zqDvjUJ8iKVytNoMNrXVtVo9AaDiozCKEhlqKurq9RoVdW2dnZVWkuzAq25RbbGTJNibWmd5t7KPWP9+vX5KpVKNOuESIXf2rCwMOPZs2cNDzz7n/7kgfx/Sd27d9dGRkZy/7qmA1i8eLF7ekJ6cFllWdfKivKuVWU1wboavS+A6GimMSONVkMatVqpgtpq3ONZ0yZICEHGujpcgoAgApYYXwZzrea+mYV5rNZKc8PW2ua6RwuXO9/++GNqs8pEZqMCRml6ze6lx2QwPvDsb//Z/E3+9u6IsCLMd+/ezT01IGL+/Pmtc7KyBlQUVwyrLK3qV11V01ZFGjK31JLWwgwLgqi2toYqSiv1WZk5hgoqNc3wh43f9Iy0ZKPydvNUOTjZaS2sLDQqlZoMOgPpqmtJr9eT1lKbY2tnfdXKyuqUg6vTub0H995pAgJ1mE+YedgTYbr/JWIe9kJNxvTX3dYjgvur5VYjIiK0y95fNqSssGRKaVnFyJqKWm+Nyoys7CxIi1VQWVGlu5V4i8kH19HUX1gaTqoeHduRR0s3cnCwJ2trSzIz18pSDOzqqmoqKSml3Ow8uhGVSAYqQlXimc6XJIud/Dqp7R3tLUDkVFWlVVRTVUNaG22ZvbP9OUdn+wPBgZ1+WfXVqiyuiKQGKTM/c+ZM7UNInSzwV3787QjBy5iBLgNiVM0Dx2yzDz8bPq2kqGROeUnlIGONkeydbcnC1kKUFJfX3E26zWMy46udUzD1G9uLAoL8qZVXS3JzcyEHR0eys0V5C0aclszMNCBZwBNqCaOR6gx1pMPsr6mpoSogp6ysjIoKiigrM4vuJSTTtQsRFJEQwUNh5BgCPILqPLzdzQ01erPS3DJSY6T2rnb3nF0c9/oH+u74csOGu1wYSTtq1Cj1sWPH5IRSsv76z78VIX28+lhdzrgsEfHJJ5/YHD98/KniguJ5FUVVIcIgyLmVE5lp1LUXIy8xmWGkaSaMnED9w/pQ+w5B5OXlTc7OTmRtYw0EmJNaw/xCAYLAfBdMzMAv8F8m+Qwf8l99QaGqI+6LV09lVRUVF5dQVnY2JcQl0rXwCNr20w6uyy3oe4f0qjPTaKwKM4vAf4zk6OlY6NLCZbefn/+6rzZ9FSU7IbLCateB7P5GAKl//l99/S0IqV8VPMtreHTDBw6dW1xY+kp5UWUXQ5WB3HxdmfHWXLl9hUmRdnDoQJo0czJ17tqJvL1bkiNWAa8AtRrCE2SnujoDLqO8F4yJP5PUgjQqDRBvRioglIUAxqBep6dSrJ7szGyKjoqjE0dP0q6DP3HLdSHtuurtbG3McpPyzFhgcG7lWOTs6vhtlz7dVy1fvjwXZVR4RwusfPl+f2Y4f1T2L0cIBmppGujkkeN6ZGblLAcihlXl1JBbW2cyt7KovXTjEiNLM/+JZ2jchFEUHNyOnF1dwAvMJbCkZGQwAAEK8BkoiiTF33/0Sr99zlKX6eKnjBSNBtKaFt8ERg8yV1JaApKWRCePn6Pln67gYiI0OFRvbm6uzorJNrN0MCdHd4c095Zuyw+eOLKJC3iRl1UGZTBS6tco5/536f/wer/bocqHfCxSKVXOmn6hfT8qKShdVJFfZWHjaE2urZx1569dkMz55edfpEnTJlJQUBD4gY3CkHkVADAMOE6NSJA//9IPE3KY5KnBfzS8coAcM6GmmuoaSk5Jo2O/nKT33v8n9yv6dO+rrymvNSu8l6+297IlO0f7XwJC2j6/ffv2FDzXgoQZ/yoS9pcgBIxazRcGZ5g1bVqH+Jh735UVVIVWl9VQq0APQ3F2CcXnxZvNnjaTnvnHk9ShUweysrGUdJpnZ+NKaOQRDIn/SQL+QQyBGiMkCZA2FhJw6UDSUpPS6Oe9h2npymU8FGP/7v3rclPztEa9kWzdbUpatfZYcPT08e38EJSBhZf/Wqn8rxGC2aExzY6RQ4fPzUjJ2lCRV21p4WBudHKzF1duXVW3opaqNbtWU/8B/cjWzpb0WA0GXFK3biBH/Fp/nHj9/NeD/r1uJGlTOmCSptWaSV5zNyqevvxiPf20/wfqEtAFzFxFpZmlGmsXK3L2cNxwIeLSc9xkvbKr/73m/5P8/+rdmiJjSL+wz3PTc8G4a1h6qisrKlMnF9xTvffGO/T4k7MgtnqAVutx8WxUksIXfm+YDaVkAf4F/iyRUYdR/7mBc+0/WQPIYX6lgmChBW+rLKuiY0dO0BNPP8XjEaHtexhLsos1ZkCaY0uHixNHThr75sdvlgYHB5vDDNSg9HLhP5P+3CibtGxaoqDH6kG9BhzISS4cV2c0CPsW9uJmbCSTLzq0dx8NHNyf1GZqSQKU6n/MmHlQDMKmYOR7NfMXFQgMt44PtkhphIoYQUYGnuQ/je3L+vKjeVuo/Z8l1DWiPxUkO62ZOZlpzSkmKpbee/NDOnb2CK8WUVNRY6yrrdPYtbBN6xQSOPS7XbuS/hukMJP908mEjLUw/L386tvnc5MLhmgsNAYrB0v17YSb6qmjJtCun3ZSaJ9QqjXUYlUYJPNUGPXDu6uHm3zIgFcBEIC1Asn6KvxbYNqCBddLStAe8QZqyGxqM+SrQf8xo02LgatrAEwVc4j6toCuhuf1zf7+F9dBYsWTBT5e4Z6tPGj4qCFkZ2nPYrLKycpFZWGtrassqHQuKi2d269/zxPnL17MCKZg83zK/9O6Sn2XSsf/yaeJTq584w2HfUdPX8xLK+po7WQFtmDUxmfG0JuLFtFLC14kJ1dHqqisBMAY5w/vhpHAAGNAG5mX4EZOcrZy8PSXwGcgQxLCxQhlCYn1Er1eB41cJ21SBmMdVg+XYboP+xdIjEmL566ZYbPgoAJ1B+eSLNxk7/33ZLM5RLgsI8XC0hILVU27f9pDT82fT57Uitz8XAyV+RVm1m7WVT5BvkMPHTt0BbUtcP0pzf7hkGo+joZfpqXIK+PbvYfDC1JLO1m7Wuqqy6vNwS9o9apVNPfJOTAIaqX4aGbG6kbzxGSFZ3njisBvZg7AAet8ajxj4Ks1mPG4r4PFtqq6moqKSignJ5cyszIpPTUdVyZlZ+RQ7v0CuhpbQmSuoj5BdtSypTu5tmxBrVt7U6vWLcnD04M8PDzJFRPEztaK1GysBOKNeiAGSIIVGDhjMvfHoGAxmYsJjIlXpKWFJf16/AyNnzSZrMiOgtr668syy7S2LWwqfdp7hx08ejQigAIs7tG9/xgpfzyKeniakME8o3fXXhfzkov7WDlb6Goqa82T8xNp87r19OjMafIF2ZLKyODZ/GACKIAQqQHUrwjcoxhr5SxymmGWM4ljRS0VgI++E0uXL12jbbukdNmsOS+IqbXUkpYPaUl1tYKevZRCtpRPFc1KKT9GDR1F/Yf0pS7dQijA35/cXFwISp9ECkt8bCphpPwhYviVMIHYbsaItIZd7dK5qzR0xCMQm22pc1Cgrii10Nze267QP6jtgP1H9sf6+PhYpqYq+tlDhtYs6z9CiIlMcc1eXXrtL0gtmqi1s9ADuGax92NU32zaRI8+NgUkhE0cBglYEy54/Lwe5CLg2VXfI5MnSUaACHOQGVbQKisqKC0tnSKu36BD+4/T0dNHGgbbvnVHkEF7xhyMGyDNsE2FZ1XRs37WtL6nJ1aYmr6IKaSF1wupL/LUsAALZjDou7K8im7FsylKURPa2QXRnMUzqd+AvtQ2sK20GquxUmoxfoOok6aWho4felM/0fgdhIHs7OzpCuxig4YMISdyJb92bWoLkwotnH2d7gX36tgPCmTef4qUP0RIvWjL5QwDe/T/NDu9cJFR1NXZu9iqbsbeUG9ev56mz5ouaToDmA2AbG9i1tuYsNQBGCZNKqGRS56FX56hTJ7YXB4dFU1Hjhynf61d01Ctd5fe4AdaqqqsJvhJqLpCJ3lONfBRBC49w8uKpvvY00gPa9QxUh6myKGMStoYX0xJpXpqgewKlLO0gmfEwYqs7YAoTIC0qPt0vzpN9jPn0Vk0dfoU6tqtIzm7OQPAgnQ1OmW1YLXK5dswot/e8MQDVyNbW3sKv3CVhjwyjPyc/AXMLLri9GILFz+XM9fvXh+GlWc0UZnfttKY84cIQVFLXDUjBg57PDMt+7vSvCryau9puHwj3OzzTz6lec88QXVgqmx/UoOBMzIUVID+Y7RyaaMXOad48LjMzeF8gvW2vKySbt+8Q7u+30Nbtm+Vo+rWrhtME3ZUXlpBJfnlZKiBhMY2J/AU5j2uuL9WYKAdA9xoKswY5uiPmQ/mAmlA14FBOpJZTWOPZVKflmaUB72H+ZPkF9CwmSTZu9qSA1abQV9Hl2+Gy34nTphE856eSz1DQ8nOwY50tdWwEOvRpyJMcCEmwQ/1UMqHRlilbenMyfM0etxY6hLY1Vinq6urLa3RurZxWhd+6+oLXCzsDzT6f4sQLy8YzzIyqieMHt0pKT79YkFSiX1AD1/dxesXzN9c+BotfvtVyOdq0tfWAmDgGXhZvDEQAoaJpa8CH7Ewh6DBdAr/GTn4BAKNlJhwD4jYR6tWf8LjpN5de8s2CrILqaqkBoZGmP3QNjfJCXyU7IGU1GojdXLS0sEwb7iqsFTAb2TjXAaWYUZKvt6MJl7MpvDiaupua0aFYOCoaipG8MWTobaOzK215N7KFTjUEOxsKEC06OVFNHP2YxQY6Ie2kIF+pdjLD3HPnkueVM14DSYkv5saFcytrGj/7oM0+4nHqX9of31uSp5Wa6khd+8WT569cvZbtMJuBh40WvltYiL70MTLKykpqfbMmSVmu3de312YXNa2TWev2vAblyymjpxE//zwHbIBCajV1cHbBtoA4AEiPFK5MjTgCzzIlGSYG1PvU35eIV6mViqIJ389QyNGjaVLl8Ope3Ao+bXxpfyMQmKbF7+omUW9PtFkZIwQB/SRXGygR1pZ0jhPW4Q0gGcxdIAIJoqMbg1+YyHQ9aJaup1TTT52WirDuBRuwjjBPxYggAROpfll7Bqmdu2DqFWLVrT759206euvqXNwJ3IC4y/IL6CU1BRKz8ig2iodOTs5y/qS//FsQX/CEr4aCyuQYkwQICewXRC52LvQlh3faOBSqMlLKDQjrRgU2rfvwcTEuNx/p6PUzz85toYPNhTi4md1oONLCjKK37d2sKqrrqmixMwEze3I69Q2yJ8qa6rBwLWkyU4nI/wXRteWZKzVSabO/oZDB4/RU8883dBuaLtQCuoYRDv3wCYUFEItIKKmxKdRSVY52bqAvgPgUjKrnzsmKY2RxFmWACSvrlSAN+ERH2pthYkGvQDMSU4E2RHKpELMCriQRT4Ygw0AX4YVYeJoDENGi0z44rbrQLrgNCMXL0cwZB/KycylG7GRNGXUZAo/douyKVkWd4e+sePIFurTvydscSBnLJmBx6mK8kkFPidatpRT3xLmlDIg+b23loEUb6aBPQdWpd+8b+3a1uVYREzEaG7s90jXQ1dIxtkM8yIq0g/vP7hXfm7J5tpKnZmbt4v+VtxN7aH9+6l7z+5UXV1FGpAj7e1rZLH2XUIsD9V5+5LK1Z1VAroJ3jB52jR0raVXF7xK3UK60dXjkXQ55iLyBOUU5lBSahK1b9+B3Fu7SYWrEkBhALEOwjDj2cyJ8xiQOl4lAHhGXjVBOSYfKw14l5EqQZIq4RWsxn1GpZG2JxbTmaQy8rfXUglWixnq1DeotKc0DrKFFQbS5ezpRK2DvKV/5Gz4GcouyJblYu/FUrfQEJoxcyZcBR3o4q0zdPPMHZowfSwkKxsIdjDZF2SRxeYvyeLoNjLaOpPw9ZcGSeZDgYEBdHTjabqfmabxDvIyVOSXBwW1b5t7PzsjAmIwzzHlBWVvygdTyWapHnO1mJ2q7sHdlpZlVVr6d29dfe7KOasP/7kUFtvepDew7QzKG5ip5uwvZJ50m0T0RdK1CSBVUHvSQ8yMjLwp2z14YB/I0wgMspbefPcNKiwspHSItjfwfM/OPXTx2nlZLrBlOwro6kclRWVUmF4IZLMDCUICyBG7XzmkRwtpqRbKXT8PZ9piNKPbuebkyaQHr8YI45s0APim2pb6drKgknLQe0hnOpBVKRigPS6nByK4eAsfd+kevhl+g0oJyiXS6CFjaPjYERTcMZi8vbzI0cmRXFydqQDjdrC3pdX/+kz651tAIlNByaTYu6TdvYnUWPlm339Mdd37EkHiqoG7OAB86PM9y2ny1GlqtVlbiIhqbVlx1Qfjho07eujkoXR0xwsCdK4xPYgQ1dmzZxlzNKjPoMfLi6pGtGjrYjx35aJ5n+A+NB2Kn9bSgmoqqyQJMCJKpK7HANJfXEMqjy6kCu6OfBVVIcDgXnyK7KVtUFtIVVp52UCJ8vL2opAuITR8xHD6x3PP0L2kJDp3+jy9+/47lJAVJ+sM6jOYSotKKT+lkGxcrcnNy5UsEV2iAw/KSMykaEwATo1aivzZ7CMfv/w8AqmVvweMgmZSFynKKSZdpYE8A1ogxMi8YTJ09AmhFa+uoF59elJrn9Zkb+8gx9u0QY8WLahdu/YyKz8vD6QVS88AG5dvWzIMGk6aeyeobs5KImsrYFyHCaRIngOg67y16A1a8dnHlgNCB1TmJOa55dnmvYGGWOpiZDA1RWNKarZkuhOC1yhSP3PmTKfo69EXKoprgt1bu1RDxLU6uH8vDX1kCNXCo2bEC6pZE0fHGiiC6ow0UCYtGcAU2YZUXl5Ozz+ziA4c20/30+9LJEBaQxRIFTmBKbq5uZr6l98cyJaVmUnXr16ndV9uotMXT8j8QX0HU0FOAUUnm+ILiNpYBVLY5H7k5+9LLT1aAHj26NNc8h5GWElpKWXAvJIQn0h79h/Dmilv6KtH517QSSzpwtVzMu/xmU9AopohtfcWAPiD6f79+1hRRpAnOyiPzrRv9z6a8ugU2r71G5o6bRLVoD/MelLngcSVl5HwagOeYkVG5PFSNICs29raUVxsHE3pOYNS6rIMXYM6mFVjQnv7eIaeuHgmEn02Q0jTMTByJIIGdO/7kr9LoOgX2h89knHB88+LnKw0UVZaIApK8kRJaoIojY4UJUU5orSkQBSVF4nCskJRUJAtKiuKxO1bEbzKxPCBI0ReTj6onxAgUWLcIxOELbmK3bv2iMrKKpkPzV5+mz6Ki4vF8V9OiHEjJ8k2uJ1FrywW+/bsF1G3ojCOHPRRIWBeMVX5zTdMNwLhPyIrI1NERkSKHdt3iifnPNXQ3nPznhdXwq+gHWUMDzaQkpIili75QJZf+8XnIjfnvixy9PBRmTdhxDiRknhPVFWUiEK8cxHgIt+/tFAUl+SL0vhboiQpBvcForQ4R1SUF4hN6zfIuqEdQys7tGgvugV12YV3MyUJd9MP07ckX7w6EEgW37FNZxHYun0VHooLZ0+LKgC8EB2XxNwQNa/MFLqhIM97N8sBFBfmiIL8LFGBMkn3YsWMKdNl50/MehJIKxE6nU5E370rdmzbIfO5zadmzxPxsfHyRY1gEBAjm8GluLhIXAbQkpJS/i3wm1X6Nz8QpyXu3Lkjrl25LqD5NysJpbbh9/Fjx2FLsJfjnD1ttvj1+FGRnpYkn588eaph/LOnzRH301IAbMAF714EGBRhspafPCh0wx1F9fwRovjqWVEIpFRVFIqEuBgxauAYrl/Xo0OosV3LduKRfo+E1gO/ASG8XDhxhmQuGSnpk2oq9IHQZGsT0mMt2ePXrn07MHIYrrEUNUlxpL3xPWntgsni0Hekqi6DtAGvGtNpkKTPP11DP+xVkM9mFDNzM6kl6yEm9u7Xg65fv0ovP7+AvtmxhYIg+589dRarWxkG3loRezEYR0cn6t2nF/n5+UBhVIRBVjlMZZp/wxADRZTpevN8fn8lcVhRp06dqEevULICP2pajrVvNuWvW7NOCiB1VEYb126gd5e9SS29PcETZWiZtO5ya92CutGO3dtp1UefgnkjJBU8kvUglRFG1aunSFtnQZaJx8ks8jIga0T9WvJq5UkznmCpk9S11boaDk0qLi1dwBlIPFCJFBNC+Fv89NNPmoriyqetbCwoMzmLGY1q2MihUABtpJSk4hgpPzCx4HFUlxlNxrDJsPjbQtpSojf27ztIX21YR2NHjEVV6A3wG7DRUCpR6I8jOhycbOmV116kFR8tl2UGDxtMRw4dBVKga6Ad/ubUFGB8z20ovI/H3ghoRSRumGBMuptc9W2h/IPtmerxN1uXP3h/Gb2w4AUa2H0AHTt6jEaNHyX75CAMrsuJ+SOn1Ph0CusdRms2raXvtu5Af+Cp7GBB2KO+ez/SV+WSwQuTv3MXsBhMSBgtWVXqDYfdI/2G0Z2UOxYWtpZUXVY5Y8qYKX6y0foPE0Ikl/9m0zcDqspr+1jZWxlTi5MtFr70CuRvIABiroChTaXDt5cP1SxaTlWbLpF+7DQyYCAsRd26cZueff4FaN7dEbqpiJAcXsMvrAADShQmOkthHP8UExUjh+BKbuSEwLgHU1OAKUA2IcL0zXYpUy0FYIwoZZXw65jyeOqxHtP8MtXkb96d4OysCBp3ofRG3Y2mwqIiaYVWyikdsWGSE0enFOYWUY/gHrTozcV0/lw4WWPyCVCBul4DqPbrK1Tz7kYydOpORiin7IqogU+npZcnTXx0Ajeh1ulqa2D5McvPzZ3DGUiycUYI38jRFxcWP8Y2qdIiCPCoNGTwIGma1kH7lhF/KCng66hzQ8BCQHtopeZkgRflwb/92gcAtiWkML30LaC+bJUbV2Y3m9ktIEndoEdGjKLtP+6gN159g+5k3aG+EA0ZabIPORQT0E3fDDQ5Xtms6aPprDflmb5Nz5RXa2zH9Nz0zeU0Giivi1+m0yfPwJdiSa+/vpjWfrGBiorLIZVZwzIj56u0BCv1gFwgpxTGz9aOvjRz3OPQrbJgMLWCogq9ySeIDJ4+CGFlLoC6ILnwqMI2pqFefXuSjyqA7qbcNdNAj4FXdTbGwDRZdmJaIbRw3jxnRFZMsLKzpLj70drxI8ZTe0QU8mA4nFP6qhl3TO+BdUQzg1TBd6A2p8MHjtKFiHPUu1t3yk8ukiZ4Hjg7fUyOHy1mycWzl2jGrNn8iLZs3ELLVn5EnvDoscxuArgCSOYFjRcDlfN/mzhPAdZvn3FO8zYeVob7NbU9eGgYRcdH0OQxU2jz1o20ZtUayssugmphI6vWSUMm4Iulzj3XAg6Org5QKgtpyzfbsUJYCob5B6QZ2iivS5TCxZMN1KIG2yB8fVvTtIUTuT0z9GvQVeoDRg0fFcYZ7OpoQMidhKSBdTrhYTRKNVw7aNgAcmvhinZrpStVGg6VhYS1A76AjrRQrpKSk+n5BS9T57ZdKDs1j6wcLaR2zR1UlFdKYcDGxkZq7k8+PY+zEXz2M3zRT4EmwzsIZLDZ/t/NZBPAZOX/44fShmmlMBIbEWxCCrsQAgL9ad3mddCjXqTte7bT2s83UHUlS/9EENXlt7k9exrZsqwm8Fzq6NeZVn6ygq5fiyQrIA+hf3JVYJbJ8igslQ0DqAvDol//3jK/PLdC0t2SgpKpnIH4NineyJGVFleMNYeClRmbzeuMQkI6YglC2YPhTQ3fMX4gt74DvsMtD2rPT/u5uJwZykyWnzIvJysPq1VN2dnZtGLJZzLvx+0/0PjJ4+U9rwwlCEL+lKtCuft7PhtXHa+45n0wUliaY6S08HCnd5e+TXMem0s/7t9BJ35RFFU2JnICj20gy2x3Y9MOp80bv8P2h3LJe5gyyMQdQRhQMQwxkbn99rAG9+nUm1KqktlGTjUV1SM2btwozfJyhaxevdBKV6sfrDFXUy5lm42DlOTTprUEOBweEG0rlAuWXSZdzBOs0EESfBpLl39Infw7U0lembQ9McpMM9rCxpzS09Np/569FJtym1Z9+DFieiGZIbFRUFkZ8ic+HoCQKftv+1bImULyuG+lf+ZjDDRPT096Z8nbFAR37yuLXiHoL1QBFzMntquZAM62Nn6XQM929P2eHXTt6jUpXUK3QouMDMAPPhRVRYk04egMtRLhYSMHc1NqhOuAbtX5Ht59oLvM4I8zJ+51BJb99Ab2JJBZaM9ukDqc4VcQZJabQebb1pD5plWkzkpFExDxuCsg5Zdjp7i6nG0qzBQeHJvQOcqPUyVI1g87fqSFC5bSkL4DaQ4iUthTyAjllWNKjMAHZ6zp2d/7zf3yVT+b0RmvFEW4IEiYgfTV7rVyCOvWrKUTx34le3KhWpAwE29kUsErRw3yy+nAnoOwolRglbDDDk62kmLSYpWZf/k+mcXHAXlQB6AHdYHLmBOEIPjVBBWVFA/h37IVT1fPKTVVtaOqyqp1xVVFZs/Nf5qCQzpRja6aLE4cJuvv3yftuetU18KD6oK7wtNmSdn3M+iVZ/5JdmZ2Cka4NZA0fiF9jZ5cHdyoAgM7fPQsNhMW0nc7voUFtZMsJUuinClxncbE94ws/uar+crhss3LowjSw/KUJ7/3aerHtDoUEd3UFk8abrMFVoqjnQt9+sUqKs4pI3vYtTjfRAW4PN+bASHm5RZ07tY5GjNmDPmAedfAV2MeeZWsv3iWzFPvkLESEhg8o1rY3wzVOtq8+SAoj17Y2tvwHNBl5WfvlNMUW8D6cNRfckE6oiZaUBsfH3RaD+cWnmS0A81kVYFFOXzx5pfbt+5SSnEs2bkoEkhTsPFqwTKUfmtGxmerVlK37nJFytXxIPD4hTiPNXblUoCu5DHgGhOKPjQ1BdBDCzyQaWq7sc/mBRhCjBQbGyvsYRlNY8ImUFJ2Atk6gmmzONtkWNwWv699K0TFIF2+eFV6RmWMBCwORp9eJGDjFK39SAU+zeZ/DxhGx4/qC+jkQt/mwApDp4ULF1pByRYqfZWuszKcWnW/YT2ILZ9G6YkDrQ/tTTWLd1LNqt1k6D8UfnKOAqmi8xevyCrKng4eX/0I8VWnM5KThwNdvXWFBvcdQMNgarexsZMziZUrhbk2QpZf6PcSB7UpQOPFzOUUMvN75X+b31i/EfimFciluU32VD6kZv24fHzb0PS5UhCSkpYltGwmz00Tex3ZhMKJxfvCvCKyRDywPqAd1by8jKo+2k6GsY9CIrACBamSgRwduwRzcailcDsbDK2iIqMC1HMfndsSG/J9mQkhaQKD2pIdlpQexE6NGWK0wvaB3gPI0G8w6cHIzSGJZGOH68Hvj0HH9pQv0mx2YpzYsC9drdzglEcnQ0Dw4dsmiV+GAdskC7c8I/lq2p6CLAVhTe+b1/yjXyaE87dycT/MvPmb0+9NCh6LOexg3cFXp06YSvHpMXJvi4KQJpMDzTJfcSEPOnzyMDb9pJIZeIgRy8TQuRvpHxlDdc5uLM3IPrVQkv39A2TXDAqVUEN2qmivLiwtbIMMa2xQYYyovdu0IuzplsuSY2phaoV2DokBop30f4BpJychcKHkHtm72XKDzRIPyhY7pm7H3aQBXfsg3qmbDI9RCvHq4DsFEzwB+YVNm3aYTPDFwGH7EgOseTLNdklpmz/63V9NgIYy3CZf3A+LufxtyuMmmk4GDAToYwQK8BIPBMKF4Z71qypEl7APBj/4MZ7zmPW1erijXTiDcDqFomPxOyKflWmGn2wN9fjdW8LgyAnw5WBOFKlrp64sK/flVvU10O2RPDzdITbXGwS5Fl/omQfK2iaTqHjsYOXEllyWtprOLqnRcx2k/lAuW3m3wnMTABVEcH9ch2cnf3MIKQOGkcBhqJzY/mPSC2RGkw8JiCa//+iW++DxMxXgNvnivuEWkPmmvAcnAL8FjxhThKxgq2rfoQN19e2OQI9YTFreDynfRJbhMbDOJvNxfxfbFirKIW0hGJB5BMMQLyvLyneHWu+CeGNO+kogCyQQiqOfGWhfay5YXVzDfWPzpZO0jvC96UX4uaKZmsm93/ExcfxYIoiZmSnxS1tYm1NJQZnMatc+EORPkUqUMoxzRoYCEEYCv9WNyBsUfuky3YdXkb1prq5u1KVrVxo4qL/0aStiqYJEpZ3//JMnAwOf++J+k5OS6OKFcEpMvEfFsMHZY3zBnTpSWNggbCpqhdEok4/fXUkKIDmyvmVLD+rarwvdTImU5fAqMimrCG8CoJru796IpuJCtO/YBlYmaPoSGRLEqKNMRu67o3dnSKz5UtmE6tHKDINtya2WGivxaUeO8CejN5l4cKbEQOGQH45TisEWL05y0PWY5988IPZVx6bHUfe23agV9lLw1gDl5epXGQBjAhC7er/4/F/0z/ff4+q/SUP6DqPVaz+DD76znMmM8EZA/ab4bzIakYEJAFK6Z9ceGfb6m4LIYH6448i3NHz0cCbosr+mfXFbdghy8G/nJ6vDpyFFXTYamhKXZ/JrTY508cZFys/PJ9+2vqbHyreCXwkDKxguvQNa0d37seSugt5nMLio4UlzYT5fTaWqQI/W0taCNdZQmZeaXLrIYktwSXEpXb9xF6gDg0JqRJlyz2QNlJr82vtJPzS/CA9UwRt/K7NVD3LBPghGxoF9PyMYLZV2bvtetsnBc93bh9Lp8JNYKSEUdedufRsP9iaLP+RDKcermsV5Tj/9sEsiY+OGTZSaAuvB3gPIVVNQi/bU2bczYuazacSYEdi2dlzOcp40TRMvGJaiWraS85eqChAGBd2DJ0lDQhkmWy0QYMcpv7CkyXsDjryCUJ63WfDk5cMQXN25rF5KWpjijmpEZDjyQwaia0snuRlFDoYBiQGoUYllZ/YZ8KDKSsug6BWTrabe64aarLmz/ZNbMQ3QvYUbJDzYbxoGzE8V0ocbOnrkGH36+Se0acPXNGHSeEhibWjmnBm0asWnFBkdgTB/axrUaxAXpdUff0alCMhmssNj43Fwr9weI5y/m19KnmkscN3SjDkz6f13lyAWeR70LG+aOHkCrfniS4rPjZXj5gnAafTYcZScnCz5DM920yphUsR9OUGv8KI2VFpbIcfDrwe1S178hsxjrSAWcyqBls58ixHAcakqhiUkNhV4GEOLeaeDAygSkoSOWuWiBiNDHCj/JJU1TO9s0uAAMCw+Mrt2kTQ71pEqMw2NoEGUY7MAJy1iZhHMTtbQK7IRqJaFKDZ7CAMYgXxuC/rIG/OZ6SuJkaGssoqKcmlS4XyX+tlUXwh2HiX6gwWGdIT89IKk9u333xIDlRMLDQwEZeU1IsX02/QtAYUXZ0Hh7Olzsi6L32weNyU3d3d5y9GQ5Yg0DPZlS4Iem3BOynxFZ5KwkfjmO3b/ugY4I5alHG1hfwiwkVOLyHvoXjZMHTC5NTCbcGKmXgdvoZzYJQUwofxAZsf2sg9XYpAD+OSk5dfBQgY/16qxp4NpimxAi8GyyxWoI01yApmveY2sV75H5jvWkroMcbdw5FQBmJzYh16LF4lG8HM/mNwHOVlQVAkHoCkvwLYcnlVSwkB5hWwpyElPz6Bd+36U7az57EuKiY6R0tXtW7dp9crV1FLdGuE/haTD9gMcnSTLRcPDKBHBPfBNfeJ702XK429TmcKCQrpw5pJ8tOFf67H55zJLMxQXFydXJ8zYclZzFCM7kDjdiLghTxUyrUjO4/Z4tjMftYL2DiEVU5YoutxIPfHuoQ4WdLcczjkgxORZ1OHwGw5Al5P75CGyXvsiWb/xDKkunEWDgDnIKSOVE8MHdVVm6BRUXTEfMzlQCA/Ks6xuYUdqH5SGZskOfG5DD4xz0mJmxHFsZ7mBlg3zBG00oyOp9/BEARYPnkkhW0IfTKU4AIZToFsQXb9yVUYJjhsxkQ4dZ7pOcqaWF1UQNpJikEr9vLxc+PV10jgpC+GDgWQiKZxnQkLTvPKKMoq8GEU22N1UlFtMffv3RXTiKGwGOiabCfLqIENK+YepXg7cBRxDZoWgN1Ob8BNBMIAuhn8GQIzBWMmkHptVlndzl4g4eSAZijQCO+qBzL50NCD5BoH5qxxbYRlkkgrwVKMu+wllsDjaQjEeAVQJC/M66CD8C4FfrIyhIpAhfNqS7rEFZLgF3/mo6VTn6CrNKVAoZdm4mjr6qosbDXLRUns24Rg1dGtyIJ0uEXQzAfMHXjPeKsabM02zxvRy7NjhVFlUjR1M7eQ9IyMksItkihzjq7WCHxoIUHrjyYTR169kWQEfJgD+3m/OZ6Zu7Yh95kCwKyIgrWGLOnr6BFwGIXJcfF6WIoiYWmHXD9P5RtLGTwwALpM/dmdTSZWcdm8FuFDf7q7UwQ5lAdCbUwLpZHYZvZZdIxvjHVyCkQgebBg4FPAFqWI+2G8gYhRAziEA8IkRnBg22AMjABp1Rf1LCw7L14MpYaRkNId/HPvyVIMfQSNQbnhXESarKfKCEEPLg+jgAF4CGz+nIAQ//1qszOji/GLp4jTgGSt7LFGYAOjmxhIafCvgMRXYmMNwDgnqSlWlvEkGvAu+Zp4xRgxYXU9GfHx8fhPeWQtvZtSdKMrLYUeYBsHNgeTr74u25XTjIYFpOlLnbsGIfrzFExAW21IZeV9VXi2t0uwukAnPTKvZ399PRkRyvmnMvJWCt+ux17A0PQdPnLGrzkjt7bALDNSDU3tbbBbCjq16giP9H8zA6/D+whPW37nPoj0o1yA1YN5Sk68En0ES8LHAh2VWqzbodaWSo4CWFiQWUS1ikJh2onfQJ6yUOmASMxVVMDo1/MuQAZBCbTX04vlsuoPNNaBfEvN3SyrptSt5FILnUXHJ2DNYKel1DZYoJ0UEFXKH7Av/eBbn6KSSk7sT1ZTpqSS7VG4LYA8ck6mK3EryDPQg9jpy6tKli/zmmaQoiqDfd6KpR48eNGbcGBo5eiQFBXSThk+GvGk1urg4U9iQQbIukyAWWgrSiiSZ4pXBZBV+beU4QQgSnAYMGig9mbwiGNGcODaLHVPF4EkJlIeAb1dacBabUrF1DowAL6ehqMJKevdKFg2zVpDM/I8RIMfCSEF9aYzFRGKzFJP/YqgRnIBG/ihU41zDQr6zQ7RemiEJL1QpZywon1JMIkIZFM8WjlXlVIhzr1p621A+cHUwtYx+TCvH9jEMxAmGfXsfWHpZMSqUs64CdJwTMzv5kmCMzzz7tMyLi0ogvxBfcoR12BzR7SxdsdjoH+ojLaKxiOtds/pLCuqgkDae/MqsFeQOV+ubr71DXQO70rC+Q2nbzg1yRSnLX3HHcicjR4+gzgFdETN8igK7BVALfzf4dDCzMZEsrC1k8LW7lzvhBAqaN2ceDQoLk2MzUUheHRzGw8JABvxAnKph73N2s8I7C9qdVkZ70iop3wA4YbtcIZRGThx3zAiVagXrQzx2XDyxWVrkdgtyCxFWZM9hW7wQyiAKqbJZMrI3t6FyXQEO9WIpipHBNZFQme8ZCLyX0M4BgXHkAH9wMXVysaWXruVTfD5oIzQRJ0gbwWaQr1s6Q2FJpeTEFGjZHRHRWC73k7BmKre+YZaz9n345yM0dsIYOnf5DHnb+pCrN3RUdM27Zi9FXOTe6a3Fb9GsubPhCW1ibpHDV+HEOS8aP3EMAgw+oin+U2gsHEOWoP/K6lB85LzaWsMdvXnnJurZqwcdP/0LJp8r+Qe1lsivRSTItdtXZF8TEGnz3tL3cECONSYS272UFVNaCn0C5KoCloU7t+/KsnqMx13U0DNX8ygtjxGATaYO8AQ6WlJqWoEs4+gMHYMxwIhgMEq44ifGxNIsDvuktDv3QfzgmsA/kPUi6CZm97mApYMFNueR3AfBoT8m0U02AygpLksh7Tl9sde7qLiY3Fq5UwV4y/gADZVm5FJyuY6qcISuS/0y512pQ4eHYaZj1hQWAICtJWKZyTPgx4wfDRtWOH2HEJqNW9bT/dhU7k6mEYNG0tynH0eZsdLexIBlUsrA5snBfCklOQX7FJWw1b2Q70f+MJImTplErjibkZOpLEOkR89Qio2Ope93/ED/WrEW26RvyDL80dmvKz390pM09dGp5NnSUyJDkm08qwUjLsLeEAsEfHB8wIEj++HCg80LvJatf5Xgef09cQANjKiF+G3LyjB4oGOCHcJHeTeAGQQsA7JYkVb4K/fJtjE2HaXWJlFrBz/5TjiBFSel2VqnGnNLIFmwVkeSQTKdk/ReYhYrA7SUB8Qud94ZVVlRTTEpUbi4hpK6tu9Ojli+FZDYWDho792BDvyyn6bPmUp9+vai0tIiuZ+bmSyzKBOA+/TtI8XeF156FkgrksubDZK8j4RPYeDEzFYqrJi1LG0lJiTS1i1bacWqT/FUT4tffo2uX7hGz2C/yc/7DuFkhbGI/5qBfTO2sh9lZgpqB7LH+1DmPDEbp0LkyDgpG1gE2BzSunVriXDTuHjCYJTE4nYdLLN6bLm+hu0SnDyD3KmsgBVDNXmA59XAIHu4ftOoLIAPN6At6lYMrAIwR1ljLybrOSjLk8Q0UYoKimVxPg6X4a3WmqWauTm6pWepsqthcmdtx3g/LUNdAxrIIUA8C5n+MfNTQ5U8fOAYzZw7Rzay+JXXqP/A/lL9v3L5Gn244gMKAhKYD7A9x8FFcWee/OUsdezYEcqUBWVlZQCxljIqg1/YtFKY1nYKqXdaytYbPxTSofAwVjJ51uRCqlqxaoUs9M3mrTTt0WmUnZWFM63WIrb4X3Tn1yiaNHUyNA9bhTxA0WUgMLA5yIIPC+DrwcR98cpQeBRsUfl5oASFCPx2oFicy/jtp9/jCA0EOQA+FtDYeaLWgedFRF2lZ+Y9S6MhWPDmoNs3byuIn/cEfVW6hh6bPVUKDUoEKO/iUhCTnZUth8DwZbe3laVVAg9UExLUJa5LQFesB9KPHjpapNyLF1VVJSI/L1NuM6isLBZHDx/k5/I6cugInjfurYCMLrZt2S6f9ezYS0CEFQN6DhIh/iEyb9vW70Rq6j1xN/qWSEiME7w1gBMAJC8weuih2JKAf8iV95gM8lsWrP/g8pxqa3QiIT5BpKakNitTVlYqYmLuiszMTNlSfbVmX9wPbFSyX37AbfJv2X99+5xfUJAr7kTdEIkJ0dhKcVu89PyL8l1g+JTv1sG3o2jfpoPMW7X8Y1Fev9+F6/L2i0sXwkV7uyD5fOe2bXJvTUlxLuCZLQoLs3Ekbr5454235fPAlu1FJ7/OYvrkyT0khnp17rW3i39XoSXbGltyw2aWC0JXWy6ys9LlBpykxFjx6CRlz8ehg4e5T5kYgNXVyl6LvLx8KJ6uIiRQQQIjb/ig4cIReZ29uoqLFy6IlLRE+ZIJaK+8vMzUTANiTAhqePCQGxNSmj5ihD4s/2F5pnqmvpp+8zNo5CInJ1NE3b0lomNuiZSURLF542YJuF4hvXkTk7zn9xvUc7C8T0pMls1iIyxYS+NGoquXr8nnCBwRtyIjRXVVqcjLzcAmp3yRkZ4iJo2Rm5KMQUBI16Cu+UuWvOIo+QZEv2tMp71dPUQFOPt92JowWSTd5uUVczcWx9vton88OZ8GDx0ikciBDgmIM7qH/SKZ2amUfj8N9uIC6j+0HySRKPpo6Qo6ce4Ede7Zge5k3KTN67eAt1QT7zOsgUx/PyOdcnOzwR9g/0Ifposbx9vxJ9/+JpmWO5dh8wybNFi05HwAV9aV9fGc8x6WZPP1D0z98ndVVQWCplPh84Giiee21raIromip//xNPXo2BP7G7Ol9PfFJ5/TPXhNu/XsKltJS02nnPwcSsSu3cR78ZJZ84PuPbrRV198Bdt4Lt28cUeyAOaFPF72K506cgkSlitsuQhEt9DGLVnyRYlEiL2t9VUWaW2dYMIFJBITkiXDs4SszadDJ9xLkR2PnzQBDEox9uXm5cBwaIDxz5rSkjNoPTrmxLtXO3XuSK+//RodPfwLTkg4j32BbaTFduumbxFkpoMuYwsRWg+JLhd+kGTpyOEYYk4mAJlERJn5wIepjGLZbVQCTfSfn6OhB2qZEK08MrXBiK+sqqTMTJx/AimKD8BhoNkg/iomOo4WPPYKhbTtStfvXqN2Xfwp8nokvfTqAojNAfIwT+5kzecw48ck4Nhz9o5CUi3Il5ODAd+jt0KFIiIjZOQjGzC577S0NGwLymP+BCsKW30tpOwtBW3vtr63U5Iz89GAG9o3RF6/ZVYAMbWNjxfcnCWUkX5fvhz7xzkhwlF2mJmehbjXU3T+xCW6Gh1O08ZPpS5gzmzQs8PW4FFw+Ny9G0NPTp9PadFptPqr1bAE6Oip554kFzdHyOGVsOXUUF5+FpgnArWh2fJLWVkqJ1mzZZWTsmIUZMmM+g8JePbEYMrzrG+KA85TnptqKKI2S05sP+IjMniyVWJVsGgL6ssdSR+4Fmaj6ziRaNGs18gGCuvtxJv02iuv02uYZKYNq6VlxdStaxeaMXkW/bBvJ8XdjqdRU0diq8UwCmrXFlSgCu9i2+BeuBV+U5pd7LF/ne17d+/EyIHhzHkYSjRYDDZnOEOuEPxNjWJw+HA2djmQq2Hvod1yLzljk4HPcjgnC7hjOWkgtXBE+PuLP6JP4GSKiFYUq+Ejh8Mv7Q3ylUI5uZkoKXBIcnvae+wHmjZhhqy7AZHl7yx6l6Jvx6A9K+kQYwWMyQ3HzubmZkHeTwYpjKe09CQJuOaAlc00++DnTZHBD5vWYWQhB6SkDM6nJOgvSXT/frpcoQw4Xksc/GdjbSMNmvv2/ExTp06FH0NFsWmwFGAFfLBiqUQGWxq4bmZGJrnCfDJw2EBunEoyS+Qe9g/fXCZ9Ruy65sSTKsA9hGLu3JU7mC3g9GPx/hKC6ZCMCObQ4JiP4o6+XS5zBiNErm07R6ujOJCefNoqqyAGYh6LrxydjkBsLitpIH9DSJPiYXjUBQrx70KPDBrF2fTM8/Np7097cd4IjpbAMd4ZGfdhKtHh+HAv2vDNOnoLZg4O1f/l7DHYn8bRd5u3Uy5sVTx4PjaPd2Kx8VJxQgnoLiWwHCiyev0wZT9/5kNZKcoq483/lTANwagEHUJFFujLElH9fDIcOx5uIRpz6TvL6NXXFsouUsuTEat7gF54+SUpqvPpFWlpKZj9lZgotbRl0zf03PP/kGW7hHWGO6EdRd4JlzFpfGgmJ0Yg28BgWIe1XPGnpCan0tGTRyBAt4SlUA3KYHl+5fqV/KIglvXJ3d3jlNFM6LAKoGpS3YUzF3GqQgnovQ0GYyFLlUA751nPiRWeiSOn0O2kWzgjaiitWPqxzH/19UW0esUXcltbNUhBFvQDJg/Ozo60bPlSRMIfkEoTF166/AMa1n807dz6AyXEJmCFsAkG5m8giBHDs8v0YsoKUPqWhf7th1KuKdni+rwKmK5roYtwmBFbtvPzCmTEy8cffgK37iTac3CvbHn2Y3MQyhNNE6ZMkKuvtAz73zMUYSf5XiotenExffDhB7Lsqo9WwA09jhLy42js2Mnk6+PbMLoyXpWFd8g3yIf5BKI69Tig7aZ87u7thIEKCA9WstOwsDC5OBqQEtoh9GgXvy7C26INm2fF5UvnsIW4WLz3piIv79q5Q9RAtGNdgdPe3Qf4zcXAXoPEwX2HxI7vdoq+nfvJvM7+3cVhiMiJifHQQZIE3LayDn/gT0eIdWvWi+6BPWVZboOv7h16iIULXhHrv1wrzp4+Le5BHwIZk/VA59FOMtpLaBC1TQ2y3M+ib2MyipKSIhEbGy2yszMbdJX8/BwRHRUl9u/ZJz5aukzMnj6rWf88hjmPPS6w6ROboBp1pYKCApGQEAcdJ0psWLuuoc7kURPF4f37xflTp8XgPkNk/o7vdshhsDhdq9eJw4ePyPxpE6eJgpw0kZWaKAZ0G8B5dSE4ZrajT8fSGTNmuOI3J4kQ/pA3g/sPfrwdZOLeIb3ZwyJWr/pEVFeWih+2K0rfsveXiPv3UyGrK/u6IfqKF+a/3DDAHdt2ir279uFF5zTkbdm0BfJ8jEhKuSeKsPccgd0NcINnThw8cFC8sfhN0b/7wIY63PcctJGUdA+oV5CfcT9dAoT1AwayKYGsQWe4IyBuSoWM88E0gcxEERN9B0iJgiJWIIvr9DXi0sWLzfrBCT9i+qQZ4ovVXwjs7QCyFURwBdYrsrOzoMzGC2znFq8vek3WDWzRTnz43vvi0pkz4sDefcKVvGT+POy9R1SO7IsRgj+LIb761xr5bPmyD4UOk/vYwf3yt59TQE0nnxAc+N9tG96XU8PCaPgxZ84c907+nTIRgcGVatpq/EQ6NOw7t27IRoLdgrGZ/wIOCygCUmS/0DwLxWsL35DPUUdgqYtl//xQjB8+QbTzaS/z337jXREefknEQSHMzM6Qs5dnfNOEoDURcT1CTJ8yU9Z5/513oUTlyCLQW3AgQYIIv3RRXLkaLnLysuurGgH4BHH7TiSuCGjXyqkRrOUnJiaKi+fOiYiIqyIr6z5WkDIRoqHJTx0/TfaxaMFikXwvBauez9ppTGyFKCzMlyv7bswdcfjQITEKFgx+P5xoLd5+9XWx/etvxJOzHpd5nP/S/BdxcoQyLqgQUHzLBY4KEXNnzJVlzpz6RZSX5IqFLyyQv7u1667v4NVBDOqHoGkl/RYhnN+vW79V7XD0Q5+ufcD9SPz4/Q5gvQBk613Z0L8++1yw5s4kgc0NnOArEAf2HRCzHm0coD009GDMgGGDHpH1hvcfLrZs/kacO39W3Lp9E7pOvEhLS5WznQHJs/j82QvCyzpAlt+7exe0XgWIlZUVIuLaNTF84EgxbvgYaNJZsl8GMiMEEhlmcUyzlXM5/LJs58MlH4qU5HsCfEzWKcGRG0veWyafjRk2Fkdv3BD5IEnZaDMtPU0kpySJWJy6wIg8/stx8dEHK1BWK9q6BYqh/YeIYK9g4UPesj7DZ9qEaeKH738U5WUKSVaQUYLVnSh2gmJwmUmjJ4hCmKEir4XL32DmNRCGRJeAToq1EoWQJJVSDP4snLNDA3TN1cNhe1F+0QKEpLBrUL/u043awYPDaBz8DstWfkgvL1pIPn5tKKQrzMowxtnZOkgJaQKUxj79+9HsJ2bR+TPnae/O/RSdehsXWkE6gQNl+BrWfxj1G9QXJnI3ybSZ4Wfcv48NMZ8pBfE5BM6m9sEdIXIgih59sGctBxH3J87/gkNhPaVllAuzqKyY4VNlJEhQIBs0+Z2hLEKy4ZSalirjo/jsXn7kAIWvGzRoTkcQpX4k9DD52belaf+YQi0QFsTCBJ/qkJ6aRjvW7sH+jSxZNjE/gfji1Mm3Gy2d84z0r7An0wMhppxYvygvL5USZlpyGm1Yt0HmT58+SVqsjxw+Ln/7dGglquBddfa0V7RpBRnKgGUJfPCWXNN975Cem9u5txe9OveUq2THN1tx3keyWLXiY64kOvt2g7HxMPhCoiRB8I004w2IZRXx8fFY6ofF6k9W41yTpwRMD7Iu1/+9a8Tg4fLZymXLYUBUZjSTNliJcWDNbvnshfkvwCakkDsATpw7d17mz350tkgD0zcJHPFxCTJ/yrgpsKOdE8XFhYKNoJzSwI/GDh0n3MhD2tt+bzycj+Ba0b/rAPHiP14U69duFL+eOCmSk1Kw4hrJHAsUpaWl4BkZWK0J4gyY/MzJM2T/r730qkgEL/t5rzJ+b+vWNTgOSgT7Bsfjr9Mp/vD61YH+IBzXp/o/OcG/Dc7urhvwV3Hm1FTqrFuqvfQvP/Wmds+ZnfKv4eRlFdCnaz6heWOfo8+2ryTedGLnABdnbSWZwwnD2375SA0OOOCLt3eVlJTAr4BgzdwcKsB3cVExdIxS7d4L5gAAFfVJREFU7BqqkWEwgUGBMthh3j+ekqMZOnwobDvs+cNZujCpsMJYWKB44TgCnU34nABbPOM5g6A0iKW82jiPlUIHmMz79RhMew/tpXnz58I1XQ6x00bGXrWG8joef1Dm8KlDVJTdGodW/kzlVWWUeT8LrgboJ4hPdrR3Imf4493c4dlogdOy3VzlMU2ys/oPGFZluBCEBWj/PE4cgYhjodZ+vp5OXviV5k6fA3/Qo6TDSj5w4LCs1cLXQ5TjwAGnFrafbtq0iaVZSapM7TYghDOwSgQQQ0d/PRrRJ6TPNwVphc/6hHgbwm9e1v5y8AQ99ezjNOuJqfJvC674dAVCP2fRkveWytMJvGBW4XOlqnWVIDXYb4cNKWwCsYZzhk9l44s3A5kSeyB5PwX7tNkh9q8vvpSP3nvrPWyIVEgKBAcZtCA9a6mp8nnHTsH17lBFUTW9jQ2cWpA0oIjhMBj07Yg/qTdm7CN06foZys/Nh0JYDsOmnSRJbPMaPWYUAkID8Bfbrsmg6TmPz5ZeUVbkOIa3qabfMGbY+6pg7uFodu6HyakOii9PivzsXPhBoNW/s5FS9An06kuv4ojD8UCkM508eYa2bv+OdytXV5ZUWZnbaG57t/PZcSVKshB+BWYZMjVDCK8SPjEzmqJ1bm3cvkDUyNSizFLXrkEhtR9/vsoipCvC9rHnoyvOIbTBTqFKyqEly94nSF808alJcDJ1IK82XuTk7AJEWMGPXomoCpg1oBVroeQpmrgFeAcCDAAURoYecU7ffbudFr32KvmSNz351JNSaeOZzgEXvDrysap2ffOzHLB/24D6oUMMZMDAicaJDz9GBI2k44wQ1vx79O4pn12/dhNjC8YxGSXSvqRSaWHi8aR1h9cAaaPAH8fSqV9P0xAchGOuwTY0tCn5E4Cu1yOMCVE3DHzO47BTPuCTrd04QgpW60wcUJZIP39/lKJSImR/PpYwPOJvLfr5+8iD1H7aJvU+I5xjWh0mhoOT81LAmrVglqyYtDWkZgjh3GlLphmil0SrDx48GB/Wa+BnmWV5K9TKnl/j1+u+Ufv6+cs/OSeR8c4SzHxnemXxAor+OBpIsoNmO16SMU8vD/jfcQg+Ync5HtYcZIa1ZBUHBAPY7CVj8vXT9wdo5acr5YC+v/gT+Qb4SrLDZnnoLUBqjTTnpxTG0eerVjfE/vKqqkZ0IZMpTlXQ8nGeC5BUDdKkWF07BHeg4QNGEf7kHU7Dg28fARoQ2cndrQVqGLFKRtLGDV/TP559Rp6WtwH3Q4YNku5qRoAGgQhs4mcE1QDh5TAH5WG1ZWM18N9FZAPhgSP7ZP/8sXbNV1jtRAsWvkgpSamwARbDZhUuTlw4ocK5xNWF6cU2du6W+y7euLyfy0+jaardtLthdXCeacXzfUMyHYm9detWy/Wr158pSi3ubeduW3Mz6abl07OfgQkhE/aooxR+8RL1hk+cN79sQ6DCRx8va2iDb/qG9CffwDbUGtHmbthIyn8OzxYhPvzXOYvAR3795bQ8hIbLXjh7nvoPGoDpwnYflTSHs+0o9m48fOSTUAJxxDBldOjYgYtLn0MueNKvx3+l5198gQKc29GmXZ/LcCE3N+WoQS73485dNGP2YzRrymxa+PqLcC07ACEeCJxw5MfS1vTN5u9w/uPT8vc7b76Dk0i7QpJTjgbhrd18cGcO3MYM5CQEEsThaKqmadl7y+iJeU+COrTCVoc08vXzoYmjJ+E4ph60beM2SsrIrW3r42WB9VXexr9N72OnjrGpl03ZyvJu2tjv3/tIzjl59PiBnf066/zsA0Rw6461KC98ndsKD0gfKSks1eAEFUgZ83FsHj/btPFrce7seTFSOT1N5nG+6bIkB9EruLfo5C1dxmJAx34CPmjZjumDNfF70I5ZsTMpceu/3CBdrUoZI3SGVBEVdVvgT+81tL1+3ToocwnQafJMTeF4vjzx2GTFRLLsvaXQ3qMgCcXhiMHKhjJYDeLnA4cb2unTtb8I9u7c8Ns0dnhUZd7TTz4DpS9CfLLyU/l74YuvmoQ7KW1NGD1F5vu5thOeKu+6oFbt9O0924tBPfovQFuc1Ese0Mxl7r/7WLJkCdM3SdLCeg98v71nB+HnGGDsGtTdgM06YviAofC5Ky9+LzFJDmBYn6GwU2XIF922dYfMY8WpZ6deuHpCjO4lfcdoVz5btfITKHm5DYABjRaZ0KpZ2bsSHi6enDlXaWPsZJGT3ViOzSU4IRqK2zH5vIN3sPx+bPJj4s7tGyIVrmI2n5gSn69o6vOzlZ/BHx8rkpLj5bmMpjL8HR0dK556vBHB3SGqw4wk2HXbt2tfMXm8Auhzp8/JaomJKcKTfGXb8BnJPPhYxKuwAHB/fbv3F74uAVVBMLX0DO55AHkywYj4G1ZhevZvv/nPVJgK9OrU83hb50DRqU1nvQu5Gwf3HowDIAvlILDDSQ5gLgxzOF1U5iXDz4yD+mQ+2mj2/drC18XNG7eg6dfbX1CDffOpaSkSUGdOnxKTx01uqHP7hmkFGQHoWmj5CdBzYsTiVxbJMh19Owsf/EUC7mf7t99BH0nAONLlOEwfP+7Y1dDe8qUfAfh3oJUnCIT5NNjmuCybTU6dOiNmTm+0OjQd/1ToNfBncFHYz5JFj+C+sl0OuODE9q9Fr7wu8wb0GlTtY+MPjTwkddbkyV4My4CAAMV0zj/+LynMJ0ySrpdffrkFHPFpgViGnfxCOFQPh0AqLw1njRxAkHsg7E2X5MD4AzRXfL1xi1gGy+pnqz4T+/fuBzAxe3WNBkaE0oi8/DypUCXCJLNn148ipE1joMSxI780tMc38EeI5ORElPtJ9unn1lYEtAgSHXw6yt9D+j2Cgy4vi5TkONi2GkkX97niw1WyDI/9mSfmw9AYjgmQCPKXLODlbNYP3LriFkgpttmJlctXig8/+Ej89MNu2O4UQ6Ue9qqffz4k22OzUFmJUh+7psSE0XIy1YYG9xIw1tYN6x82rB72/x0yTAhEyKZ0pE+YMKFXUKv2NX27SBO7/uTxk/Il2Kb0wXsfycG98/rbAo6pZi/3sB9ch08eTYXtiBFxOfyi+Odbir0s2E9ByHqY6EHFGhKH97BV99KlCyAjvdCfRrTzBil1VZAS7NNJjmHRgkUiLvauSLgXA5qOVVzfBh9B+8SMp2QZaEoiwCZIfLtlq7h7F3wFiLmPVVVeUYpIdEWjb+j4gRuOLImJixdPwALByN24dlNDCZzRy3n6nhhfR9i9YPV4th6OTG0eKkTVP/9zXz7K3xGhYQOHTcapnHIgj46frjcxx3sJCh9Bq+LDJctAUuIQq1TeYCBk/sB0nQ2F2J0qUuAjiYm9A0RcEhu/2iCYHHJdU5gNm7KLi+pN2WC6ubm5kkzdiIwQs6bPlmXxNzqEH4x+/rj8XNqKdgBASNsu8tkn4E+M6AQcW8u+GDb6cYKkhud2MJu0EO1bKdbox6Y8Jvbs3i1u3IwUcQmxIj0jTRTCoFpZXYkx6yVJQ7SYNO+XYgXciIgQLz33kuyng0d7kVvP3xjvK5avYslJhIGkd2vb+UPcc9LU82Tl17/5/LMY4yVXi9P+5+lq6jZfuXOJYN8xQGQ0q4Jv+tihEzRt+hTZ3YSR42kS/gwS/xUFDg2Vf1sKChbvrygrRaQ8Qm34IMxvP9lJ2ZQh6wwfAM0af+8c6iQOcLmCY2VxaAt0Fjaz5OH06GroGhvXb6b1m9fL8i1garT3sFNcpHgTKPYEwyhOcVfMLOu+hP4xYgiCqjXUyqO1VBa5IsgozUf0fRecgmcNa0J4VLhsb0TYCBo0PIz8oWs5Y5sE/51ePjCA43NZGSzErtq46Fja/vUOunzniqxz+sRxGvzIcHn/6/EThuEjR5j1696fqsorv7qZcPMlPFCBiWvO/gV/llV28pAPSQeH9A57FgcC18/G1fp7ID35RfkCR76KsF7DZD7qyu9+8JCNHzFBTBwxUQyuDy4zPePv2Y/OEtu2fitWfbhcln/39XdgQFSMizBVSJ4Ref2aeOlZJXrQi3zEyDB5KLFwwUwP9AgSQZ7tZF0vcz88U3wX3Pb6NV9JR1UmDJTsWeSEPwYgQgN7y/Kb12wSX33+L5MXT+ZxPexbFyP6jxRTx04RiIoXvTv0aXjGz/t27ytOnziG1VcEwSAb7of9cmVwBGjPzj2VGYOC/2eJCnX/TDLnwsP6D3myZxflxWCF1V28dMGYAwYdHRsLfWSzePbp50RviLso2nCpyAyW31BE/Q0Rn328Whw9chhHk9+A1BPVIFae+vWUBByHeLLHjkNQ2XvJ7UyFK/RGxE2Bw9EERwYGQMgIhqOHn7303AIBU4aUgn7e1xj6uufHXSIeIaHswzGlzz/5Qtb5CBJXCpxw7K7eDYHio6Ufwu/xCEcSCoQ/NIyb2+/ZobdY8NwLYsuGDSIe7tzikmxx48Z1sfTdD6SQEwoX9IDQfqtRVqamUqop7+/8lkgZ88iY8RCBK9ARD173xqK3DEd/+UVE3rotLl++Ik79ekLs27tHfL9jO1bASuFLigOqU+suAlEbcFZFgqEmiJOnTsiXH9xnMBh9moQb/lgYgJUMx9VpOKbGyedR4AGmxKLy5LFTG4C2cV0jc+Uye37aK5+989pb4tatSOg4aRBrFYXwPJRXHnPv9r2hmEaI9PRE6CfRcDYhLgCKIT/DtgMBLVzs/PZbsW/3D+LsqV9FxNVL4sb1cKyO42LJO+9jm5n8w5FiUO8wEdY37A385sRkyky5/d9+SvI1fdz0bojjTcBOJn4Rts3UeFBr8Y95z0Ls/RoH8UfAF58M7TtOfL/9ezG8/yj5wignhvQaInbs2CE2b94s89567W0TvKXWm4TZu3+fAtgX4SYFD5LPETkOBexVWcfHzF9YwwLA7X33zbYGfwn+3JLMG9xrqHT/ZmangmwppJADF4J9ebwW4ujBn8VPP+5oiF/mdsJCB4utX38Nae2OyLwPSTDurvhp1/fiuWfmC/bDowyvCkMwlNJH+g8rHT14xKP4zcmsqW9Jyfrffko9Zflbb7mMCBu+fzC09V4gYy2oBdv6eXuvGNpjsNi+dStmKWbi/RRx9Uo4dIIVYmDoEAkwLtPesyOCva3Ee2+8h7+ogEPtyyvgBk2CrH9ALHxZAfzuH3dDJFVkWByNIes6wWqw/Zvv5OrjdviKi46RSCstKRGLFyj+/hXgT2dOn5QkkJW3e/eSxEQEO3doHSz6dRnQMA426yxFAMPFc2cwiRJEXNxNibDn601DaN/YkryrWHMfgtU8qMfA649PmdEZ+ZzM/1NpSin+N32a9BRuHn9RcxHE0XLs6YOppHddWO8wRox84UXwnh38GcohRFFEJYqz506JVR+vEmOGKgzaVI4B9Oy85xAdPrkBUM/MnQ+7lGI+YT0AO6Hks1eef1mkQoeIuHpZ4Ogk0aFlB3EFkSWmdPPGTYTtuTa0w2E+zz3zgmB6b+qPvyeCcX8MBfDkiaMgmezvTxTnz52EQvuxCHLrJMuCWdcg5qC2i19XWCw6iX4hvb9kAyzqc+LvPyu1yop/y8eoUaMatNC50+Z279e17wk2qLFE1KNDr+rOAZ1laFEABYm3sS/i2NHD4B3xIgWk7Nq1KyBb28WboPWThjciAQNFgMBkseXrLY3IgD5RBOb86ceKYW/Ju+9LfePO7YgGfrIPWnx1VQX0QWU1YSsbVuTHsCcpZg5ul69ZU2eJd15/E2R0GwSEC9IwybzkSvg5seaLL+DeVaQ1R3LT9enWtzIAf1OlrWNbEdqu++1xw0ePNQGy6YQ05f1fv/9SjMJHrIVbksfCthH1yMEj5xfmFb5ZllPehl0xTq2cqpJup5gVUK65vzqIJr08gfoO7I3jWIPk9jNWJGzgy2AXLwdBs9fRCZ5GUxABH0RQC13mZsRNWvrmUvy500vUN6w/9jEOlh68X46cxl/POUvzZj1Ji95YiAj1ttAheH88tiCjXnZWDkJT8ec14GSys8Mfl8TOrdKyAuk4wyYixBLfx7Eat+js8Yt09MwRfg9Dj049amuqdDaV+VVk42JZ6uLm8EWXvqFfIJXguQr8QgtnE/OTvyT9pQjhEQERKl+VrwUOAZSeo6efftorLT7teShsz5fmlDtY2kOYtLOouht9VwN3klxVMx+dDb9KT2xjCIYPoa/0yT/4dnxkbUVlBYK0Y+Vekx0/bXuwSLPfC194Gbt3Z5I/tq7xHj/2Ij6Y2Jl19coVSkXw9c2I23T8yEWKS7/FxfSdAjrrEARuw8cwaa01Rntnu62uHm6fHzp+yOQMsQAyDOxlfbDd/+b3X44Q02BYBo+MjGSJSw74iRlPtMtIT5+PjY5PVRXXOMh94hYa3f34+3UFlM8itKYVdreOmzdOOqr8AvywOnBcFOJ8eUbnY88F/nwR7d6xh85hFYwc9AjOnH8Of1BYBy0+D65VgdOoW8gQntdnL6VMSqLxwyfSuKlj4L7tKD2X7LGshcuY/xQ47ze/cvkKnT58hm7du8HD5rHqOsFIqTHTWvE+FjWi3x1d7Hc6Ozt8dejkMUU1x6qAA08bHR39l60K7tyU/jaEcAfw/qlCKdSM/9CYqcO5c+f656RmzSosKHkC54z48tYGPmKjpry65l5OMk7v0LH8LkOSOvl0ow5d28pAibg7CXQ56pJsZlTYGHrplWfJL8gHXBSvgCgTvnjjixGuVw42+HLlWrp4+6IsD0Mkte+IP68BV2zs3QSKjGmIT2MkGNpYtqmz87TDX95QqbEhCydPa8sdHO13Orm7bNl7cK/iKJctkRZSVB0urve3pL8VIaYR4wXUfOE3i8EyLXnlFcerUTEjCvPLZleVVwxX1alxAi2KmGGbMTa/5yIutpjkDi9GDtelwFbtQBKVgzoDOwRgSmN/PcKFOKCBAyl4lyv7wDlyJCoyhkpge+K/e5uQGy/7xIdcsU7kaHRFUJylDcL6kcNA4L0gOHrjhrOr887WbVvv27BhQ6qpEr41IE/81wv+UvLUpP2G2/8JQky91SOG+2z2YtMnTvcvyM0dWVpRNb62qqYvzrC1ZUYMKQmzGvsrYG/FUd512K8ijLo6nG2vV6WXJXOzEpam9pt8w80cANKDnSoWuLRmKpyNyNtaQLXk7hYpZyFaCSfXWd60d3I45trS9RAU1OtYJU3HpgYiVP8LRJjG/j9FiKlTfMsZj28pfjbJp6eeeqplVmpWLxwlOMigr+2t1xnb4aRsB/7rznxEN+xb8pAaCAYsQcgWeHXIhN+8yUiPM6x43zf/tVAmY3wcFJ9uh7NNKrGPPgkhSpG2DnYXW3m2uvz1tq/jgAQeR9NkGt/fRpqadtb0/v8XQkxjeLD/BwFDixcvdi/MyAmoqq4JqqquCkT4aBsc4eRRVlLx/8YNPMuQD1iZcwNzEQuodQfagsfNzfWPjZPjK3C4/AUwMp4BN+Y8AIrfAV4jfktAQuDuokWLnsIsR6IJugNJLU2ZAGJKE2C50Y4/AAAAAElFTkSuQmCC"
                      }
                    },
                    {
                      "locale": "de",
                      "textDictionary": {
                        "pullRequests": "Projekt Pull Requests",
                        "myPullRequests": "My Pull Requests",
                        "nodeName": "Github Review",
                        "renovateIcon": "data:image/png;base64,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",
                        "extensionIcon": "data:image/png;base64,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"
                      }
                    }
                  ]
              }
          }
      }`
}

func GetValidJSON_tech_docs() string {
	return `{
        "name": "tech-docs-ui",
        "luigiConfigFragment": {
              "data": {
                  "viewGroup": {
                    "preloadSuffix": "/#/preload",
                    "requiredIFramePermissions": {
                      "allow": ["clipboard-read", "clipboard-write", "fullscreen"],
                      "sandbox": ["allow-forms"]
                    }
                  },
                  "nodes": [
                    {
                      "pathSegment": "documentation",
                      "label": "{{documentation}}",
                      "icon": "documents",
                      "hideFromNav": false,
                      "virtualTree": true,
                      "urlSuffix": "/#/projects/:projectId",
                      "entityType": "project",
                      "loadingIndicator": {
                        "enabled": true
                      },
                      "visibleForContext": "serviceProviderConfig.disableTechDocsProject == null  || serviceProviderConfig.disableTechDocsProject == 'false'",
                      "clientPermissions": {
                        "urlParameters": {
                          "q": {
                            "read": true,
                            "write": true
                          }
                        }
                      }
                    },
                    {
                      "pathSegment": "documentation",
                      "label": "{{documentation}}",
                      "icon": "documents",
                      "hideFromNav": false,
                      "virtualTree": true,
                      "urlSuffix": "/#/",
                      "entityType": "project.component",
                      "loadingIndicator": {
                        "enabled": true
                      },
                      "clientPermissions": {
                        "urlParameters": {
                          "q": {
                            "read": true,
                            "write": true
                          }
                        }
                      },
                      "visibleForContext": "(serviceProviderConfig.disableTechDocsComponent == null  || serviceProviderConfig.disableTechDocsComponent == 'false') && (entityContext.component.annotations.\"techdocs.dxp.sap.com/pages-branch\" == null && entityContext.component.annotations.\"github.dxp.sap.com/acronym\" != 'ghw')"
                    },
                    {
                      "externalLink": {
                        "url": "https://pages.portal.d1.hyperspace.tools.sap/{context.entityContext.component.annotations[\"github.dxp.sap.com/acronym\"]}/{context.entityContext.component.annotations[\"github.dxp.sap.com/login\"]}/{context.entityContext.component.annotations[\"github.dxp.sap.com/repo-name\"]}/",
                        "sameWindow": false
                      },
                      "label": "{{page}}",
                      "icon": "internet-browser",
                      "hideFromNav": false,
                      "entityType": "project.component",
                      "clientPermissions": {
                        "urlParameters": {
                          "q": {
                            "read": true,
                            "write": true
                          }
                        }
                      },
                      "visibleForContext": "entityContext.component.annotations.\"techdocs.dxp.sap.com/pages-branch\" != null && entityContext.component.annotations.\"github.dxp.sap.com/acronym\" != 'ghw'"
                    }
                  ],
                  "texts": [
                    {
                      "locale": "",
                      "textDictionary": {
                        "documentation": "Documentation",
                        "page": "Page"
                      }
                    },
                    {
                      "locale": "en",
                      "textDictionary": {
                        "documentation": "Documentation",
                        "page": "Page"
                      }
                    },
                    {
                      "locale": "de",
                      "textDictionary": {
                        "documentation": "Dokumentation",
                        "page": "Page"
                      }
                    }
                  ]
              }
          }
      }`
}

func GetValidJSON_url() string {
	return `{
    "url": "https://url.com",
		"luigiConfigFragment": {
			"data": {
				"nodeDefaults": {
					"entityType": "global",
					"isolateView": true
				},
				"nodes": [
					{
						"entityType": "global",
						"icon": "home",
						"label": "Overview",
						"pathSegment": "home"
					}
				],
				"texts": [
					{
						"locale": "de",
						"textDictionary": {
							"hello": "Hallo"
						}
					}
				]
			}
		},
		"name": "overview"
	}`
}

func GetValidJSON_home_ui() string {
	return `{
  "name": "home-ui",
  "luigiConfigFragment": {
    "data": {
      "viewGroup": {
        "preloadSuffix": "/#/preload"
      },
      "nodes": [
        {
          "entityType": "global",
          "label": "Home",
          "icon": "home",
          "hideSideNav": true,
          "pathSegment": "home",
          "dxpOrder": 1,
          "order": 1,
          "urlSuffix": "/main.js#home",
          "defineEntity": {
            "id": "home"
          },
          "compound": {},
          "webcomponent": {
            "selfRegistered": true
          }
        },
        {
          "pathSegment": "users",
          "label": "Users",
          "hideSideNav": true,
          "hideFromNav": true,
          "entityType": "global",
          "navigationContext": "users",
          "children": [
            {
              "pathSegment": ":profileUserId",
              "hideFromNav": true,
              "defineEntity": {
                "id": "user",
                "contextKey": "profileUserId",
                "label": "User",
                "pluralLabel": "Users"
              },
              "context": {
                "profileUserId": ":profileUserId"
              },
              "navigationContext": "user"
            }
          ]
        },
        {
          "entityType": "user",
          "pathSegment": "overview",
          "hideSideNav": true,
          "showBreadcrumbs": false,
          "defineEntity": {
            "id": "overview"
          },
          "webcomponent": true,
          "urlSuffix": "/microfrontends/dynamicPageDashboard.js",
          "context": {
            "columns": [
              {
                "max": "655px",
                "layout": "minmax(0,1fr)",
                "ignoreItemLayout": true
              },
              {
                "min": "656px",
                "max": "975px",
                "layout": "minmax(0,1fr)",
                "ignoreItemLayout": true
              },
              {
                "min": "976px",
                "max": "1359px",
                "layout": "minmax(0,1fr) minmax(0,1fr)",
                "ignoreItemLayout": true
              },
              {
                "min": "1360px",
                "max": "1679px",
                "layout": "minmax(0,1fr) minmax(0,1fr)",
                "ignoreItemLayout": true
              },
              {
                "min": "1680px",
                "max": "1999px",
                "layout": "minmax(0,1fr) minmax(0,1fr) minmax(0,1fr) minmax(0,1fr)"
              },
              {
                "min": "2000px",
                "layout": "minmax(0,1fr) minmax(0,1fr) minmax(0,1fr) minmax(0,1fr) minmax(0,1fr)"
              }
            ],
            "rows": "[first] repeat(20, auto ) [last]",
            "dashboardPersistencePrefix": "dxp_bounded_context_overview",
            "headerCollapseHeight": "50px",
            "itemMargin": "10px"
          },
          "compound": {
            "children": []
          }
        },
        {
          "entityType": "project.boundedContext",
          "pathSegment": "overview",
          "label": "Overview",
          "icon": "course-book",
          "defineEntity": {
            "id": "overview"
          },
          "webcomponent": true,
          "urlSuffix": "/microfrontends/dynamicPageDashboard.js",
          "context": {
            "columns": [
              {
                "max": "655px",
                "layout": "minmax(0,1fr)",
                "ignoreItemLayout": true
              },
              {
                "min": "656px",
                "max": "975px",
                "layout": "minmax(0,1fr)",
                "ignoreItemLayout": true
              },
              {
                "min": "976px",
                "max": "1359px",
                "layout": "minmax(0,1fr) minmax(0,1fr)"
              },
              {
                "min": "1360px",
                "max": "1679px",
                "layout": "minmax(0,1fr) minmax(0,1fr) minmax(0,1fr)"
              },
              {
                "min": "1680px",
                "max": "1999px",
                "layout": "minmax(0,1fr) minmax(0,1fr) minmax(0,1fr) minmax(0,1fr)"
              },
              {
                "min": "2000px",
                "layout": "minmax(0,1fr) minmax(0,1fr) minmax(0,1fr) minmax(0,1fr) minmax(0,1fr)"
              }
            ],
            "rows": "[first] repeat(20, auto ) [last]",
            "dashboardPersistencePrefix": "dxp_bounded_context_overview",
            "headerCollapseHeight": "50px",
            "itemMargin": "10px"
          },
          "compound": {
            "children": []
          }
        },
        {
          "entityType": "project.apiDefinition",
          "pathSegment": "overview",
          "label": "Overview",
          "icon": "course-book",
          "defineEntity": {
            "id": "overview"
          },
          "webcomponent": true,
          "urlSuffix": "/microfrontends/dynamicPageDashboard.js",
          "context": {
            "columns": [
              {
                "max": "655px",
                "layout": "minmax(0,1fr)",
                "ignoreItemLayout": true
              },
              {
                "min": "656px",
                "max": "975px",
                "layout": "minmax(0,1fr)",
                "ignoreItemLayout": true
              },
              {
                "min": "976px",
                "max": "1359px",
                "layout": "minmax(0,1fr) minmax(0,1fr)"
              },
              {
                "min": "1360px",
                "max": "1679px",
                "layout": "minmax(0,1fr) minmax(0,1fr) minmax(0,1fr)"
              },
              {
                "min": "1680px",
                "max": "1999px",
                "layout": "minmax(0,1fr) minmax(0,1fr) minmax(0,1fr) minmax(0,1fr)"
              },
              {
                "min": "2000px",
                "layout": "minmax(0,1fr) minmax(0,1fr) minmax(0,1fr) minmax(0,1fr) minmax(0,1fr)"
              }
            ],
            "rows": "[first] repeat(20, auto ) [last]",
            "dashboardPersistencePrefix": "dxp_api_definition_overview",
            "headerCollapseHeight": "50px",
            "itemMargin": "10px"
          },
          "compound": {
            "children": []
          }
        },
        {
          "entityType": "team",
          "pathSegment": "overview",
          "label": "Overview",
          "icon": "business-by-design",
          "defineEntity": {
            "id": "overview"
          },
          "navSlot": "main",
          "webcomponent": true,
          "urlSuffix": "/microfrontends/dynamicPageDashboard.js",
          "context": {
            "columns": [
              {
                "max": "655px",
                "layout": "minmax(0,1fr)",
                "ignoreItemLayout": true
              },
              {
                "min": "656px",
                "max": "975px",
                "layout": "minmax(0,1fr)",
                "ignoreItemLayout": true
              },
              {
                "min": "976px",
                "max": "1359px",
                "layout": "minmax(0,1fr) minmax(0,1fr)"
              },
              {
                "min": "1360px",
                "max": "1679px",
                "layout": "minmax(0,1fr) minmax(0,1fr) minmax(0,1fr)"
              },
              {
                "min": "1680px",
                "max": "1999px",
                "layout": "minmax(0,1fr) minmax(0,1fr) minmax(0,1fr) minmax(0,1fr)"
              },
              {
                "min": "2000px",
                "layout": "minmax(0,1fr) minmax(0,1fr) minmax(0,1fr) minmax(0,1fr) minmax(0,1fr)"
              }
            ],
            "rows": "[first] repeat(20, auto ) [last]",
            "dashboardPersistencePrefix": "dxp_team_overview",
            "headerCollapseHeight": "50px",
            "itemMargin": "10px"
          },
          "compound": {
            "children": []
          }
        }
      ]
    }
  }
}`
}
