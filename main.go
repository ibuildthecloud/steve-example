package main

import (
	"log"
	"net/http"

	"github.com/rancher/apiserver/pkg/types"
	"github.com/rancher/steve/pkg/attributes"
	"github.com/rancher/steve/pkg/auth"
	"github.com/rancher/steve/pkg/schema"
	"github.com/rancher/steve/pkg/server"
	"github.com/rancher/wrangler/pkg/kubeconfig"
	"github.com/rancher/wrangler/pkg/ratelimit"
	"github.com/rancher/wrangler/pkg/signals"
	"k8s.io/apiserver/pkg/authentication/user"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	ctx := signals.SetupSignalContext()

	// Need a rest.Config, this grabs from standard locations
	restConfig, err := kubeconfig.GetNonInteractiveClientConfigWithContext("", "").ClientConfig()
	if err != nil {
		return err
	}
	restConfig.RateLimiter = ratelimit.None

	// Create steve server (which is a http.Handler) with custom auth
	steve, err := server.New(ctx, restConfig, &server.Options{
		AuthMiddleware: auth.ToMiddleware(auth.AuthenticatorFunc(MyAuth)),
	})
	if err != nil {
		return err
	}

	steve.SchemaFactory.AddTemplate(schema.Template{
		Group: "",
		Kind:  "Secret",
		// Globally disable access to all methods for this type
		Customize: func(apiSchema *types.APISchema) {
			attributes.AddDisallowMethods(apiSchema,
				http.MethodGet,
				http.MethodPost,
				http.MethodPut,
				http.MethodDelete,
				http.MethodPatch)
		},
	})

	// Add some custom stores to add custom logic on CRUD
	steve.SchemaFactory.AddTemplate(schema.Template{
		Group: "",
		Kind:  "ConfigMap",
		StoreFactory: func(store types.Store) types.Store {
			return &ConfigMapCustomStore{
				Store: store,
			}
		},
	})

	// Steve runs at /v1, /api, /apis so you can add this to a mux Router like below, instead
	// of passing it directly to the http.Server. This also gives you a way to block access to the
	// raw k8s API if you don't want users to see that, so just don't include the /api* routes
	//
	//    router := mux.NewRouter()
	//    router.PathPrefix("/v1").Handler(steve)
	//    router.PathPrefix("/api").Handler(steve)
	return http.ListenAndServe(":8080", steve)
}

type ConfigMapCustomStore struct {
	// embedded the actual store to provide GET, PUT, POST, DELETE, etc
	types.Store
}

// Create Override just the create logic to do some pre-validation. We ensure you can only create a config map if you can also create
// a secret.
func (c *ConfigMapCustomStore) Create(apiOp *types.APIRequest, schema *types.APISchema, data types.APIObject) (types.APIObject, error) {
	// resource == nameplural.apigroup  so clusters.management.cattle.io.  The core v1 group is just empty
	if err := apiOp.AccessControl.CanDo(apiOp, "secrets", "POST", "", ""); err != nil {
		return types.APIObject{}, err
	}

	// Call the embedded store to do the real work
	return c.Store.Create(apiOp, schema, data)
}

func MyAuth(req *http.Request) (user.Info, bool, error) {
	// decrypt jwt or something and return who they are
	return &user.DefaultInfo{
		Name: "bob",
		UID:  "bob",
		// Adding the group system:masters will make them a cluster admin
		Groups: []string{"system:masters"},
	}, true, nil
}
