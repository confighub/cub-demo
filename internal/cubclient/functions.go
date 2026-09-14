package cubclient

import (
	"fmt"

	"github.com/confighub/sdk/core/cubapi"
	goclient "github.com/confighub/sdk/core/openapi/goclient-new"
)

// Invocation is one function call with positional string arguments.
type Invocation struct {
	Function string
	Args     []string
}

// InvokeFunctions runs the invocations, in order, over every unit the where
// expression selects, org-wide. The server records one revision per changed
// unit with the given change description.
func (c *Client) InvokeFunctions(where, description string, invocations []Invocation) error {
	list := make(goclient.FunctionInvocationList, 0, len(invocations))
	for _, inv := range invocations {
		args := make([]goclient.FunctionArgument, 0, len(inv.Args))
		for _, a := range inv.Args {
			fa := goclient.FunctionArgument{Value: &goclient.FunctionArgument_Value{}}
			if err := fa.Value.FromFunctionArgumentValue0(a); err != nil {
				return err
			}
			args = append(args, fa)
		}
		list = append(list, goclient.FunctionInvocation{FunctionName: inv.Function, Arguments: args})
	}
	body := goclient.FunctionInvocationsRequest{
		ChangeDescription:   description,
		FunctionInvocations: &list,
		ToolchainType:       "Kubernetes/YAML",
	}
	params := &goclient.InvokeFunctionsOnOrgParams{Where: &where}
	res, err := c.api.InvokeFunctionsOnOrgWithResponse(c.ctx, params, body)
	if cubapi.IsAPIError(err, res) {
		return cubapi.InterpretErrorGeneric(err, res)
	}
	if res.StatusCode() >= 300 {
		return fmt.Errorf("invoke functions: %s", res.Status())
	}
	return nil
}
