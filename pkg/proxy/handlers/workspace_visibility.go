package handlers

import (
	"encoding/json"
	"fmt"
	"io"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	commonproxy "github.com/codeready-toolchain/toolchain-common/pkg/proxy"
	"github.com/codeready-toolchain/toolchain-common/pkg/spacebinding"
	"github.com/labstack/echo/v4"
	errs "github.com/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/selection"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func HandleWorkspaceVisibilityPatchRequest(spaceLister *SpaceLister, hostClient client.Client) echo.HandlerFunc {
	// get specific workspace
	return func(ctx echo.Context) error {
		return patchWorkspaceVisibility(ctx, spaceLister, hostClient)
	}
}

func patchWorkspaceVisibility(ctx echo.Context, spaceLister *SpaceLister, hostClient client.Client) error {
	// parse request
	b, err := io.ReadAll(ctx.Request().Body)
	if err != nil {
		ctx.Logger().Error(errs.Wrap(err, "error reading request body"))
		return errorResponse(ctx, apierrors.NewInternalError(err))
	}

	ws := toolchainv1alpha1.WorkspaceSpec{}
	if err := json.Unmarshal(b, &ws); err != nil {
		ctx.Logger().Error(errs.Wrap(err, "error unmarshaling request body to WorkspaceSpec"))
		return errorResponse(ctx, apierrors.NewInternalError(err))
	}

	// fetch space and workspace
	workspaceName := ctx.Param("workspace")
	s, w, err := getUserSpaceAndWorkspace(ctx, spaceLister, workspaceName)
	if err != nil {
		return err
	}

	// if no visibility change, return actual workspace
	if w.Spec.Visibility == ws.Visibility {
		return getWorkspaceResponse(ctx, w)
	}

	// update visibility
	s.Config.Visibility = ws.Visibility
	//TODO: impersonate requesting user to leverage on k8s RBAC
	if err := hostClient.Update(ctx.Request().Context(), s); err != nil {
		ctx.Logger().Error(errs.Wrap(err, "error patching space"))
		return errorResponse(ctx, apierrors.NewInternalError(err))
	}

	// update visibility and return workspace
	w.Spec.Visibility = ws.Visibility
	return getWorkspaceResponse(ctx, w)
}

func getUserSpaceAndWorkspace(ctx echo.Context, spaceLister *SpaceLister, workspaceName string) (*toolchainv1alpha1.Space, *toolchainv1alpha1.Workspace, error) {
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

	return space, createWorkspaceObject(userSignup.Name, space, userBinding,
		commonproxy.WithAvailableRoles(getRolesFromNSTemplateTier(nsTemplateTier)),
		commonproxy.WithBindings(bindings),
	), nil
}
