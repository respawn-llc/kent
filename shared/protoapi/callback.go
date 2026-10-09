package protoapi

import (
	"core/shared/auth"
	authpb "core/shared/protoapi/gen/kent/api/auth"
)

func CallbackTransportToProto(value auth.CallbackTransport) *authpb.BootstrapCallbackTransport {
	return &authpb.BootstrapCallbackTransport{
		BindAddress: value.BindAddress, RedirectHost: value.RedirectHost,
		CallbackPath: value.CallbackPath,
	}
}

func CallbackTransportFromProto(value *authpb.BootstrapCallbackTransport) auth.CallbackTransport {
	return auth.CallbackTransport{
		BindAddress: value.GetBindAddress(), RedirectHost: value.GetRedirectHost(),
		CallbackPath: value.GetCallbackPath(),
	}
}
