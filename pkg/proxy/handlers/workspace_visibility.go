package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"log"

	"github.com/labstack/echo/v4"
	errs "github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/api/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/registration-service/pkg/context"
	commonproxy "github.com/codeready-toolchain/toolchain-common/pkg/proxy"
	"github.com/codeready-toolchain/toolchain-common/pkg/spacebinding"
)

type ImpersonatingClientFunc func(username string) (client.Client, error)

var DefaultImpersonatingClientFuncBuilder = func(cfg *rest.Config, opts client.Options) ImpersonatingClientFunc {
	return func(username string) (client.Client, error) {
		cfg.Impersonate.UserName = username
		return client.New(cfg, opts)
	}
}

func HandleWorkspaceVisibilityPatchRequest(spaceLister *SpaceLister, hostClient client.Client, clientFunc ImpersonatingClientFunc) echo.HandlerFunc {
	// get specific workspace
	return func(ctx echo.Context) error {
		return patchWorkspaceVisibility(ctx, spaceLister, hostClient, clientFunc)
	}
}

func patchWorkspaceVisibility(ctx echo.Context, spaceLister *SpaceLister, hostClient client.Client, clientFunc ImpersonatingClientFunc) error {
	// parse request
	log.Println("reading body")
	b, err := io.ReadAll(ctx.Request().Body)
	if err != nil {
		ctx.Logger().Error(errs.Wrap(err, "error reading request body"))
		return errorResponse(ctx, apierrors.NewInternalError(err))
	}

	log.Printf("unmarshaling workspace spec from body: %s", string(b))
	ws := toolchainv1alpha1.Workspace{}
	if err := json.Unmarshal(b, &ws); err != nil {
		ctx.Logger().Error(errs.Wrap(err, "error unmarshaling request body to WorkspaceSpec"))
		return errorResponse(ctx, apierrors.NewInternalError(err))
	}

	log.Printf("unmarshaled workspace spec: %+v", ws)
	// fetch space and workspace
	log.Println("get user space and workspace")
	workspaceName := ctx.Param("workspace")
	cfg, w, err := getUserSpaceAndWorkspace(ctx, spaceLister, workspaceName)
	if err != nil {
		return err
	}

	// if no visibility change, return actual workspace
	log.Println("check visibility")
	if cfg.Spec.Visibility == ws.Spec.Visibility {
		log.Printf("same visibility (%s), no need to update it", cfg.Spec.Visibility)
		return getWorkspaceResponse(ctx, w)
	}

	// update visibility
	log.Printf("set visibility to %s", ws.Spec.Visibility)
	cfg.Spec.Visibility = ws.Spec.Visibility

	if err != nil {
		ctx.Logger().Error(errs.Wrap(err, "error building impersonating client"))
		return errorResponse(ctx, apierrors.NewInternalError(err))
	}

	// TODO: build impersonating client
	// TODO: try to be smarter here
	username := ctx.Get(context.UsernameKey).(string)
	cli, err := clientFunc(username)
	if err != nil {
		ctx.Logger().Error(errs.Wrap(err, "error building impersonating client"))
		return errorResponse(ctx, apierrors.NewInternalError(err))
	}

	// cli := hostClient
	// update space spec
	if err := cli.Update(ctx.Request().Context(), cfg, &client.UpdateOptions{}); err != nil {
		if errors.IsForbidden(err) {
			r := schema.GroupResource{Group: "toolchain.dev.openshift.com", Resource: "workspaces"}
			return errorResponse(ctx, apierrors.NewForbidden(r, "error updating workspace", err))
		}
		ctx.Logger().Error(errs.Wrap(err, "error patching space user config"))
		return errorResponse(ctx, apierrors.NewInternalError(err))
	}

	// update visibility and return workspace
	w.Spec.Visibility = ws.Spec.Visibility
	return getWorkspaceResponse(ctx, w)
}

func getUserSpaceAndWorkspace(ctx echo.Context, spaceLister *SpaceLister, workspaceName string) (*toolchainv1alpha1.SpaceUserConfig, *toolchainv1alpha1.Workspace, error) {
	userSignup, err := spaceLister.GetProvisionedUserSignup(ctx)
	if err != nil {
		ctx.Logger().Error(errs.Wrap(err, "provisioned user signup error"))
		return nil, nil, err
	}
	// signup is not ready
	if userSignup == nil {
		r := schema.GroupResource{Group: "toolchain.dev.openshift.com", Resource: "workspaces"}
		return nil, nil, apierrors.NewForbidden(r, "user is not approved yet", nil)
	}

	space, err := spaceLister.GetInformerServiceFunc().GetSpace(workspaceName)
	if err != nil {
		ctx.Logger().Error(errs.Wrap(err, "unable to get space"))
		return nil, nil, nil
	}

	// recursively get all the spacebindings for the current workspace
	listSpaceBindingsFunc := func(spaceName string) ([]toolchainv1alpha1.SpaceBinding, error) {
		spaceSelector, err := labels.NewRequirement(toolchainv1alpha1.SpaceBindingSpaceLabelKey, selection.Equals, []string{spaceName})
		if err != nil {
			return nil, err
		}
		return spaceLister.GetInformerServiceFunc().ListSpaceBindings(*spaceSelector)
	}
	spaceBindingLister := spacebinding.NewLister(listSpaceBindingsFunc, spaceLister.GetInformerServiceFunc().GetSpace)
	allSpaceBindings, err := spaceBindingLister.ListForSpace(space, []toolchainv1alpha1.SpaceBinding{})
	if err != nil {
		ctx.Logger().Error(err, "failed to list space bindings")
		return nil, nil, err
	}

	// check if user has access to this workspace
	userBinding := filterUserSpaceBinding(userSignup.CompliantUsername, allSpaceBindings)
	if userBinding == nil {
		//  let's only log the issue and consider this as not found
		ctx.Logger().Error(fmt.Sprintf("unauthorized access - there is no SpaceBinding present for the user %s and the workspace %s", userSignup.CompliantUsername, workspaceName))
		return nil, nil, nil
	}
	// build the Bindings list with the available actions
	// this field is populated only for the GET workspace request
	bindings, err := generateWorkspaceBindings(space, allSpaceBindings)
	if err != nil {
		ctx.Logger().Error(errs.Wrap(err, "unable to generate bindings field"))
		return nil, nil, err
	}

	// add available roles, this field is populated only for the GET workspace request
	nsTemplateTier, err := spaceLister.GetInformerServiceFunc().GetNSTemplateTier(space.Spec.TierName)
	if err != nil {
		ctx.Logger().Error(errs.Wrap(err, "unable to get nstemplatetier"))
		return nil, nil, err
	}

	cfg, err := spaceLister.GetInformerServiceFunc().GetSpaceUserConfig(space.Name)
	if err != nil {
		return nil, nil, err
	}

	return cfg, createWorkspaceObject(userSignup.Name, space, cfg, userBinding,
		commonproxy.WithAvailableRoles(getRolesFromNSTemplateTier(nsTemplateTier)),
		commonproxy.WithBindings(bindings),
	), nil
}
