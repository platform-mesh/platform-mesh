// Package manifests embeds the kcp bootstrap manifests that the operator
// applies when self-seeding its kcp API surface.
package manifests

import "embed"

// KCPFS holds the embedded kcp manifests (WorkspaceTypes, APIExport,
// APIResourceSchema, ...) used by the `seed` subcommand to bootstrap the kcp
// API surface and provider workspace structure.
//
//go:embed kcp/00-root/*.yaml
//go:embed kcp/01-platform-mesh-system/*.yaml
var KCPFS embed.FS

const (
	// RootDir is the directory (within KCPFS) holding the manifests applied to
	// the root workspace (the provider/providers WorkspaceTypes).
	RootDir = "kcp/00-root"

	// WorkspaceTypeProvidersFile is the parent WorkspaceType for root:providers.
	WorkspaceTypeProvidersFile = "workspacetype-providers.yaml"

	// WorkspaceTypeProviderFile is the child WorkspaceType for per-provider
	// tenant workspaces. It references the providers APIExport, so that export
	// should exist before workspaces of this type are created.
	WorkspaceTypeProviderFile = "workspacetype-provider.yaml"

	// PlatformMeshSystemDir is the directory (within KCPFS) holding the
	// root:platform-mesh-system bootstrap manifests.
	PlatformMeshSystemDir = "kcp/01-platform-mesh-system"

	// APIResourceSchemaFile is the APIResourceSchema for providers.providers.platform-mesh.io.
	// It must be applied before the APIExport that references it.
	APIResourceSchemaFile = "apiresourceschema-providers.providers.platform-mesh.io.yaml"

	// APIExportFile is the APIExport for providers.platform-mesh.io.
	APIExportFile = "apiexport-providers.platform-mesh.io.yaml"

	// ClusterRoleProvidersBindFile / ClusterRoleBindingProvidersBindFile grant
	// the `bind` verb on the providers APIExport. They must be applied before any
	// WorkspaceType or APIBinding that references the export, otherwise kcp rejects
	// the bind with "no permission to bind to export".
	ClusterRoleProvidersBindFile        = "clusterrole-providers-apiexport-bind.yaml"
	ClusterRoleBindingProvidersBindFile = "clusterrolebinding-providers-apiexport-bind.yaml"
)
