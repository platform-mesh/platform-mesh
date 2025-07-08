package subroutines

//go:generate go run -mod=mod github.com/vektra/mockery/v2 --all --case=underscore --with-expecter
//go:generate go run -mod=mod github.com/vektra/mockery/v2 --srcpkg=sigs.k8s.io/controller-runtime/pkg/client --name=Client --case=underscore --with-expecter
