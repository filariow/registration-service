package handlers_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/registration-service/pkg/configuration"
	rcontext "github.com/codeready-toolchain/registration-service/pkg/context"
	"github.com/codeready-toolchain/registration-service/pkg/metrics"
	"github.com/codeready-toolchain/registration-service/pkg/proxy/handlers"
	"github.com/codeready-toolchain/registration-service/test/fake"
	spacetest "github.com/codeready-toolchain/toolchain-common/pkg/test/space"
)

func TestWorkspaceVisibilityPatch(t *testing.T) {
	t.Run("owner can update space visibility from private to community", func(t *testing.T) {
		// Given user "owner" exists
		// And   space "home" is created by "owner"
		// And   space is private
		fakeSignupService := fake.NewSignupService(newSignup("owner", "owner", true))
		sp := spacetest.NewSpace(configuration.Namespace(), "home",
			spacetest.WithLabel(toolchainv1alpha1.SpaceCreatorLabelKey, "owner"),
		)
		cfg := &toolchainv1alpha1.SpaceUserConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "home",
				Namespace: configuration.Namespace(),
			},
			Spec: toolchainv1alpha1.SpaceUserConfigSpec{
				Visibility: toolchainv1alpha1.SpaceVisibilityCommunity,
			},
		}
		sbr := fake.NewSpaceBinding("owner-home", "owner", "home", "admin")

		fakeClient := fake.InitClient(t,
			sp,
			sbr,
			cfg,

			fake.NewBase1NSTemplateTier(),
		)

		s := &handlers.SpaceLister{
			GetSignupFunc:          fakeSignupService.GetSignupFromInformer,
			GetInformerServiceFunc: fake.GetInformerService(fakeClient),
			ProxyMetrics:           metrics.NewProxyMetrics(prometheus.NewRegistry()),
		}

		// When owner updates home workspace's visibility to community
		e := echo.New()
		req := httptest.NewRequest(http.MethodPatch, "/", strings.NewReader(`{"visibility":"community"}`))
		rec := httptest.NewRecorder()
		ctx := e.NewContext(req, rec)
		ctx.Set(rcontext.UsernameKey, "owner")
		ctx.SetParamNames("workspace")
		ctx.SetParamValues("home")

		err := handlers.HandleWorkspaceVisibilityPatchRequest(s, fakeClient, func(username string) (client.Client, error) { return fakeClient, nil })(ctx)
		require.NoError(t, err)

		// Then workspace visibility is updated to community
		require.Equal(t, http.StatusOK, rec.Result().StatusCode)
		b, err := io.ReadAll(rec.Body)
		require.NoError(t, err)

		ws := toolchainv1alpha1.Workspace{}
		require.NoError(t, json.Unmarshal(b, &ws))

		require.Equal(t, ws.Name, sp.Name)
		require.Equal(t, ws.Namespace, sp.Namespace)
		require.Equal(t, ws.Spec.Visibility, toolchainv1alpha1.SpaceVisibilityCommunity)

		st := types.NamespacedName{Namespace: sp.Namespace, Name: sp.Name}
		ucfg := toolchainv1alpha1.SpaceUserConfig{}
		require.NoError(t, fakeClient.Get(context.TODO(), st, &ucfg))
		require.Equal(t, toolchainv1alpha1.SpaceVisibilityCommunity, ucfg.Spec.Visibility)
	})

	t.Run("owner can update space visibility from community to private", func(t *testing.T) {
		// Given user "owner" exists
		// And   space "home" is created by "owner"
		// And   space is community
		fakeSignupService := fake.NewSignupService(newSignup("owner", "owner", true))
		sp := spacetest.NewSpace(configuration.Namespace(), "home",
			spacetest.WithLabel(toolchainv1alpha1.SpaceCreatorLabelKey, "owner"),
		)
		cfg := &toolchainv1alpha1.SpaceUserConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "home",
				Namespace: configuration.Namespace(),
			},
			Spec: toolchainv1alpha1.SpaceUserConfigSpec{
				Visibility: toolchainv1alpha1.SpaceVisibilityCommunity,
			},
		}
		sbr := fake.NewSpaceBinding("owner-home", "owner", "home", "admin")

		fakeClient := fake.InitClient(t,
			sp,
			sbr,
			cfg,

			fake.NewBase1NSTemplateTier(),
		)

		s := &handlers.SpaceLister{
			GetSignupFunc:          fakeSignupService.GetSignupFromInformer,
			GetInformerServiceFunc: fake.GetInformerService(fakeClient),
			ProxyMetrics:           metrics.NewProxyMetrics(prometheus.NewRegistry()),
		}

		// When owner updates home workspace's visibility to private
		e := echo.New()
		req := httptest.NewRequest(http.MethodPatch, "/", strings.NewReader(`{"visibility":"private"}`))
		rec := httptest.NewRecorder()
		ctx := e.NewContext(req, rec)
		ctx.Set(rcontext.UsernameKey, "owner")
		ctx.SetParamNames("workspace")
		ctx.SetParamValues("home")

		err := handlers.HandleWorkspaceVisibilityPatchRequest(s, fakeClient, func(username string) (client.Client, error) { return fakeClient, nil })(ctx)
		require.NoError(t, err)

		// Then workspace visibility is updated to private
		require.Equal(t, http.StatusOK, rec.Result().StatusCode)
		b, err := io.ReadAll(rec.Body)
		require.NoError(t, err)

		ws := toolchainv1alpha1.Workspace{}
		require.NoError(t, json.Unmarshal(b, &ws))

		require.Equal(t, ws.Name, sp.Name)
		require.Equal(t, ws.Namespace, sp.Namespace)
		require.Equal(t, ws.Spec.Visibility, toolchainv1alpha1.SpaceVisibilityPrivate)

		st := types.NamespacedName{Namespace: sp.Namespace, Name: sp.Name}
		ucfg := toolchainv1alpha1.SpaceUserConfig{}
		require.NoError(t, fakeClient.Get(context.TODO(), st, &ucfg))
		require.Equal(t, toolchainv1alpha1.SpaceVisibilityPrivate, ucfg.Spec.Visibility)
	})

	t.Run("maintainer user can update space visibility", func(t *testing.T) {
		// Given user "owner" exists
		// And   space "owner" is created by "owner"
		// And   space is private
		// And   user "maintainer" exists
		// And   "maintainer" has role "maintainer" on space "owner"
		fakeSignupService := fake.NewSignupService(
			newSignup("owner", "owner", true),
			newSignup("maintainer", "maintainer", true),
		)
		sp := spacetest.NewSpace(configuration.Namespace(), "owner",
			spacetest.WithLabel(toolchainv1alpha1.SpaceCreatorLabelKey, "owner"),
		)
		cfg := &toolchainv1alpha1.SpaceUserConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "owner",
				Namespace: configuration.Namespace(),
			},
			Spec: toolchainv1alpha1.SpaceUserConfigSpec{
				Visibility: toolchainv1alpha1.SpaceVisibilityCommunity,
			},
		}
		sbrOwner := fake.NewSpaceBinding("owner:owner", "owner", "owner", "admin")
		sbrMaintainer := fake.NewSpaceBinding("maintainer:owner", "maintainer", "owner", "maintainer")

		fakeClient := fake.InitClient(t,
			sp,
			sbrOwner,
			sbrMaintainer,
			cfg,

			fake.NewBase1NSTemplateTier(),
		)

		s := &handlers.SpaceLister{
			GetSignupFunc:          fakeSignupService.GetSignupFromInformer,
			GetInformerServiceFunc: fake.GetInformerService(fakeClient),
			ProxyMetrics:           metrics.NewProxyMetrics(prometheus.NewRegistry()),
		}

		// When "maintainer" updates "home" workspace's visibility to "private"
		e := echo.New()
		req := httptest.NewRequest(http.MethodPatch, "/", strings.NewReader(`{"visibility":"private"}`))
		rec := httptest.NewRecorder()
		ctx := e.NewContext(req, rec)
		ctx.Set(rcontext.UsernameKey, "maintainer")
		ctx.SetParamNames("workspace")
		ctx.SetParamValues("owner")

		err := handlers.HandleWorkspaceVisibilityPatchRequest(s, fakeClient, func(username string) (client.Client, error) { return fakeClient, nil })(ctx)
		require.NoError(t, err)

		// Then workspace visibility is updated to "private"
		require.Equal(t, http.StatusOK, rec.Result().StatusCode)
		b, err := io.ReadAll(rec.Body)
		require.NoError(t, err)

		ws := toolchainv1alpha1.Workspace{}
		require.NoError(t, json.Unmarshal(b, &ws))

		require.Equal(t, ws.Name, sp.Name)
		require.Equal(t, ws.Namespace, sp.Namespace)
		require.Equal(t, ws.Spec.Visibility, toolchainv1alpha1.SpaceVisibilityPrivate)

		st := types.NamespacedName{Namespace: sp.Namespace, Name: sp.Name}
		ucfg := toolchainv1alpha1.SpaceUserConfig{}
		require.NoError(t, fakeClient.Get(context.TODO(), st, &ucfg))
		require.Equal(t, toolchainv1alpha1.SpaceVisibilityPrivate, ucfg.Spec.Visibility)
	})

	t.Run("viewer user cannot update space visibility", func(t *testing.T) {
		// Given user "owner" exists
		// And   space "owner" is created by "owner"
		// And   space is private
		// And   user "viewer" exists
		// And   "viewer" has role "viewer" on space "owner"
		fakeSignupService := fake.NewSignupService(
			newSignup("owner", "owner", true),
			newSignup("viewer", "viewer", true),
		)
		sp := spacetest.NewSpace(configuration.Namespace(), "owner",
			spacetest.WithLabel(toolchainv1alpha1.SpaceCreatorLabelKey, "owner"),
		)
		cfg := &toolchainv1alpha1.SpaceUserConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "owner",
				Namespace: configuration.Namespace(),
			},
			Spec: toolchainv1alpha1.SpaceUserConfigSpec{
				Visibility: toolchainv1alpha1.SpaceVisibilityCommunity,
			},
		}
		sbrOwner := fake.NewSpaceBinding("owner:owner", "owner", "owner", "admin")
		sbrViewer := fake.NewSpaceBinding("viewer:owner", "viewer", "owner", "viewer")

		fakeClient := fake.InitClient(t,
			sp,
			sbrOwner,
			sbrViewer,
			cfg,

			fake.NewBase1NSTemplateTier(),
		)

		s := &handlers.SpaceLister{
			GetSignupFunc:          fakeSignupService.GetSignupFromInformer,
			GetInformerServiceFunc: fake.GetInformerService(fakeClient),
			ProxyMetrics:           metrics.NewProxyMetrics(prometheus.NewRegistry()),
		}

		// When "maintainer" updates "home" workspace's visibility to "private"
		e := echo.New()
		req := httptest.NewRequest(http.MethodPatch, "/", strings.NewReader(`{"visibility":"private"}`))
		rec := httptest.NewRecorder()
		ctx := e.NewContext(req, rec)
		ctx.Set(rcontext.UsernameKey, "viewer")
		ctx.SetParamNames("workspace")
		ctx.SetParamValues("owner")

		err := handlers.HandleWorkspaceVisibilityPatchRequest(s, fakeClient, func(username string) (client.Client, error) {
			cl := fake.InitClient(t, sp, cfg)
			cl.MockUpdate = func(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
				switch username {
				case "viewer":
					r := schema.GroupResource{Group: "toolchain.dev.openshift.com", Resource: "look_for_me_in_tests"}
					return errors.NewForbidden(r, "viewer does not have write access to any resource", fmt.Errorf("error"))
				default:
					return cl.Client.Update(ctx, obj, opts...)
				}
			}
			return cl, nil
		})(ctx)
		require.NoError(t, err)

		// Then workspace visibility is updated to "private"
		require.Equal(t, http.StatusForbidden, rec.Result().StatusCode)
	})
}
