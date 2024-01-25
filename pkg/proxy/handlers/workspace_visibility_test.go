package handlers_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/registration-service/pkg/configuration"
	rcontext "github.com/codeready-toolchain/registration-service/pkg/context"
	"github.com/codeready-toolchain/registration-service/pkg/metrics"
	"github.com/codeready-toolchain/registration-service/pkg/proxy/handlers"
	"github.com/codeready-toolchain/registration-service/test/fake"
	spacetest "github.com/codeready-toolchain/toolchain-common/pkg/test/space"
	"github.com/labstack/echo/v4"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/types"
)

func TestWorkspaceVisibilityPatch(t *testing.T) {
	t.Run("owner can update space visibility", func(t *testing.T) {
		// Given user "owner" exists
		// And   space "home" is created by "owner"
		fakeSignupService := fake.NewSignupService(newSignup("owner", "owner", true))
		sp := spacetest.NewSpace(configuration.Namespace(), "home",
			spacetest.WithLabel(toolchainv1alpha1.SpaceCreatorLabelKey, "owner"),
		)
		sbr := fake.NewSpaceBinding("owner-home", "owner", "home", "admin")

		fakeClient := fake.InitClient(t,
			sp,
			sbr,

			fake.NewBase1NSTemplateTier(),
		)

		signupProvider := fakeSignupService.GetSignupFromInformer
		informerFunc := fake.GetInformerService(fakeClient)
		proxyMetrics := metrics.NewProxyMetrics(prometheus.NewRegistry())

		s := &handlers.SpaceLister{
			GetSignupFunc:          signupProvider,
			GetInformerServiceFunc: informerFunc,
			ProxyMetrics:           proxyMetrics,
		}

		// When owner updates home workspace's visibility
		e := echo.New()
		req := httptest.NewRequest(http.MethodPatch, "/", strings.NewReader(`{"config":{"visibility": "community"}}`))
		rec := httptest.NewRecorder()
		ctx := e.NewContext(req, rec)
		ctx.Set(rcontext.UsernameKey, "owner")
		ctx.SetParamNames("workspace")
		ctx.SetParamValues("home")

		err := handlers.HandleWorkspaceVisibilityPatchRequest(s, fakeClient)(ctx)
		require.NoError(t, err)

		// Then workspace visibility is updated
		nsp := toolchainv1alpha1.Space{}
		nspt := types.NamespacedName{Namespace: sp.Namespace, Name: sp.Name}
		err = fakeClient.Get(context.TODO(), nspt, &nsp)
		require.NoError(t, err)
		require.Equal(t, nsp.Config.Visibility, toolchainv1alpha1.SpaceVisibilityCommunity)
	})

	t.Run("admin user can update space visibility", func(t *testing.T) {})

	t.Run("non-admin user cannot update space visibility", func(t *testing.T) {})
}
