package ocm

import (
	"strings"

	"github.com/mandelsoft/goutils/errors"
	"github.com/opencontainers/go-digest"
	"go.platform-mesh.io/provider-operator/pkg/ocm/grammar"
)

type RefSpec struct {
	UniformRepositorySpec `json:",inline"`
	ArtSpec               `json:",inline"`
}

type ArtSpec struct {
	// Repository is the part of a reference without its hostname
	Repository string `json:"repository"`
	// artifact version
	ArtVersion `json:",inline"`
}

type ArtVersion struct {
	// +optional
	Tag *string `json:"tag,omitempty"`
	// +optional
	Digest *digest.Digest `json:"digest,omitempty"`
}

type UniformRepositorySpec struct {
	// Type
	Type string `json:"type,omitempty"`
	// Scheme
	Scheme string `json:"scheme,omitempty"`
	// Host is the hostname of an oci ref.
	Host string `json:"host,omitempty"`
	// Info is the file path used to host ctf component versions
	Info string `json:"info,omitempty"`

	// CreateIfMissing indicates whether a file based or dynamic repo should be created if it does not exist
	CreateIfMissing bool `json:"createIfMissing,omitempty"`
	// TypeHint should be set if CreateIfMissing is true to help to decide what kind of repo to create
	TypeHint string `json:"typeHint,omitempty"`
}

func (r *RefSpec) SetType(t string) {
	r.Type = t
}

func pointer(b []byte) *string {
	if len(b) == 0 {
		return nil
	}
	s := string(b)
	return &s
}

func dig(b []byte) *digest.Digest {
	if len(b) == 0 {
		return nil
	}
	s := digest.Digest(b)
	return &s
}

// to find a suitable secret for images on Docker Hub, we need its two domains to do matching.
const (
	dockerHubDomain       = "docker.io"
	dockerHubLegacyDomain = "index.docker.io"

	KIND_OCI_REFERENCE       = "oci reference"
	KIND_ARETEFACT_REFERENCE = "artifact reference"
)

func ParseRef(ref string) (RefSpec, error) {
	create := false
	if strings.HasPrefix(ref, "+") {
		create = true
		ref = ref[1:]
	}

	spec := RefSpec{UniformRepositorySpec: UniformRepositorySpec{CreateIfMissing: create}}
	match := grammar.AnchoredTypedSchemedHostPortArtifactRegexp.FindSubmatch([]byte(ref))
	if match != nil {
		spec.SetType(string(match[1]))
		spec.Scheme = string(match[2])
		spec.Host = string(match[3])
		spec.Repository = string(match[4])
		spec.Tag = pointer(match[5])
		spec.Digest = dig(match[6])
		return spec, nil
	}

	match = grammar.AnchoredTypedOptSchemedReqHostReqPortArtifactRegexp.FindSubmatch([]byte(ref))
	if match != nil {
		spec.SetType(string(match[1]))
		spec.Scheme = string(match[2])
		spec.Host = string(match[3])
		spec.Repository = string(match[4])
		spec.Tag = pointer(match[5])
		spec.Digest = dig(match[6])
		return spec, nil
	}
	match = grammar.FileReferenceRegexp.FindSubmatch([]byte(ref))
	if match != nil {
		spec.SetType(string(match[1]))
		spec.Info = string(match[2])
		spec.Repository = string(match[3])
		spec.Tag = pointer(match[4])
		spec.Digest = dig(match[5])
		return spec, nil
	}
	match = grammar.DockerLibraryReferenceRegexp.FindSubmatch([]byte(ref))
	if match != nil {
		spec.Host = dockerHubDomain
		spec.Repository = "library" + grammar.RepositorySeparator + string(match[1])
		spec.Tag = pointer(match[2])
		spec.Digest = dig(match[3])
		return spec, nil
	}
	match = grammar.DockerReferenceRegexp.FindSubmatch([]byte(ref))
	if match != nil {
		spec.Host = dockerHubDomain
		spec.Repository = string(match[1])
		spec.Tag = pointer(match[2])
		spec.Digest = dig(match[3])
		return spec, nil
	}
	match = grammar.ReferenceRegexp.FindSubmatch([]byte(ref))
	if match != nil {
		spec.Scheme = string(match[1])
		spec.Host = string(match[2])
		spec.Repository = string(match[3])
		spec.Tag = pointer(match[4])
		spec.Digest = dig(match[5])
		return spec, nil
	}
	match = grammar.TypedReferenceRegexp.FindSubmatch([]byte(ref))
	if match != nil {
		spec.SetType(string(match[1]))
		spec.Scheme = string(match[2])
		spec.Host = string(match[3])
		spec.Repository = string(match[4])
		spec.Tag = pointer(match[5])
		spec.Digest = dig(match[6])
		return spec, nil
	}
	match = grammar.TypedURIRegexp.FindSubmatch([]byte(ref))
	if match != nil {
		spec.SetType(string(match[1]))
		spec.Scheme = string(match[2])
		spec.Host = string(match[3])
		spec.Repository = string(match[4])
		spec.Tag = pointer(match[5])
		spec.Digest = dig(match[6])
		return spec, nil
	}
	match = grammar.TypedGenericReferenceRegexp.FindSubmatch([]byte(ref))
	if match != nil {
		spec.SetType(string(match[1]))
		spec.Info = string(match[2])
		spec.Repository = string(match[3])
		spec.Tag = pointer(match[4])
		spec.Digest = dig(match[5])
		return spec, nil
	}
	match = grammar.AnchoredRegistryRegexp.FindSubmatch([]byte(ref))
	if match != nil {
		spec.SetType(string(match[1]))
		spec.Info = string(match[2])
		spec.Repository = string(match[3])
		return spec, nil
	}

	match = grammar.AnchoredGenericRegistryRegexp.FindSubmatch([]byte(ref))
	if match != nil {
		spec.SetType(string(match[1]))
		spec.Info = string(match[2])

		match = grammar.ErrorCheckRegexp.FindSubmatch([]byte(ref))
		if match != nil {
			if len(match[3]) != 0 || len(match[4]) != 0 {
				return RefSpec{}, errors.ErrInvalid(KIND_OCI_REFERENCE, ref)
			}
		}
		return spec, nil
	}
	return RefSpec{}, errors.ErrInvalid(KIND_OCI_REFERENCE, ref)
}
