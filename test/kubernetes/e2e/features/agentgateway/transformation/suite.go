package transformation

import (
	"context"
	"fmt"
	"net/http"

	"github.com/stretchr/testify/suite"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/kgateway-dev/kgateway/v2/pkg/utils/kubeutils"
	"github.com/kgateway-dev/kgateway/v2/pkg/utils/requestutils/curl"
	testmatchers "github.com/kgateway-dev/kgateway/v2/test/gomega/matchers"
	"github.com/kgateway-dev/kgateway/v2/test/kubernetes/e2e"
	"github.com/kgateway-dev/kgateway/v2/test/kubernetes/e2e/defaults"
	"github.com/kgateway-dev/kgateway/v2/test/kubernetes/e2e/tests/base"
)

var _ e2e.NewSuiteFunc = NewTestingSuite

// testingSuite is a suite of basic routing / "happy path" tests
type testingSuite struct {
	*base.BaseTestingSuite
}

func NewTestingSuite(ctx context.Context, testInst *e2e.TestInstallation) suite.TestingSuite {
	// Define the setup TestCase for common resources
	setupTestCase := base.TestCase{
		Manifests: []string{
			defaults.CurlPodManifest,
			simpleServiceManifest,
			gatewayManifest,
			transformForHeadersManifest,
			transformForBodyJsonManifest,
			transformForBodyAsStringManifest,
			gatewayAttachedTransformManifest,
		},
		Resources: []client.Object{
			// resources from curl manifest
			defaults.CurlPod,
			// resources from service manifest
			simpleSvc, simpleDeployment,
			// resources from gateway manifest
			gateway,
			// deployer-generated resources
			proxyDeployment, proxyService, proxyServiceAccount,
			// routes and traffic policies
			routeForHeaders, routeForBodyJson, routeForBodyAsString, routeBasic,
			trafficPolicyForHeaders, trafficPolicyForBodyJson, trafficPolicyForBodyAsString, trafficPolicyForGatewayAttachedTransform,
		},
	}

	// everything is applied during setup; there are no additional test-specific manifests
	testCases := map[string]base.TestCase{}

	return &testingSuite{
		BaseTestingSuite: base.NewBaseTestingSuite(ctx, testInst, setupTestCase, testCases),
	}
}

func (s *testingSuite) SetupSuite() {
	s.BaseTestingSuite.SetupSuite()

	// Wait for the backend service to be ready - use the correct label selector
	s.TestInstallation.Assertions.EventuallyPodsRunning(s.Ctx, simpleDeployment.GetNamespace(), metav1.ListOptions{
		LabelSelector: "app.kubernetes.io/name=backend-0",
	})

	// Wait for the agent gateway proxy pod to be running and ready
	s.TestInstallation.Assertions.EventuallyPodsRunning(s.Ctx, proxyObjectMeta.GetNamespace(), metav1.ListOptions{
		LabelSelector: fmt.Sprintf("app.kubernetes.io/name=%s", proxyObjectMeta.GetName()),
	})
}

// TestTransformationForHeaders tests header-based transformations
func (s *testingSuite) TestTransformationForHeaders() {

	testCases := []struct {
		name      string
		routeName string
		opts      []curl.Option
		resp      *testmatchers.HttpResponse
	}{
		{
			name:      "basic",
			routeName: "headers",
			opts: []curl.Option{
				curl.WithBody("hello"),
			},
			resp: &testmatchers.HttpResponse{
				StatusCode: http.StatusOK,
				Headers: map[string]interface{}{
					"x-foo-response": "notsuper",
				},
			},
		},
		{
			name:      "conditional set by request header", // CEL and the request header access
			routeName: "headers",
			opts: []curl.Option{
				curl.WithBody("hello"),
				curl.WithHeader("x-add-bar", "super"),
			},
			resp: &testmatchers.HttpResponse{
				StatusCode: http.StatusOK,
				Headers: map[string]interface{}{
					"x-foo-response": "supersupersuper",
				},
			},
		},
	}

	for _, tc := range testCases {
		s.TestInstallation.Assertions.AssertEventualCurlResponse(
			s.Ctx,
			defaults.CurlPodExecOpt,
			append(tc.opts,
				curl.WithHost(kubeutils.ServiceFQDN(proxyObjectMeta)),
				curl.WithHostHeader(fmt.Sprintf("example-%s.com", tc.routeName)),
				curl.WithPort(8080),
			),
			tc.resp)
	}
}

// TestTransformationForBodyJson tests JSON body parsing transformations
func (s *testingSuite) TestTransformationForBodyJson() {

	testCases := []struct {
		name      string
		routeName string
		opts      []curl.Option
		resp      *testmatchers.HttpResponse
	}{
		{
			name:      "pull json info", // shows we parse the body as json
			routeName: "route-for-body-json",
			opts: []curl.Option{
				curl.WithBody(`{"mykey": {"myinnerkey": "myinnervalue"}}`),
				curl.WithHeader("X-Incoming-Stuff", "super"),
			},
			resp: &testmatchers.HttpResponse{
				StatusCode: http.StatusOK,
				Headers: map[string]interface{}{
					"x-how-great":   "level_super",
					"from-incoming": "key_level_myinnervalue",
				},
			},
		},
		{
			name:      "dont pull json info if not json", // shows we parse the body as json
			routeName: "route-for-body-json",
			opts: []curl.Option{
				curl.WithBody("hello"),
			},
			resp: &testmatchers.HttpResponse{
				StatusCode: http.StatusBadRequest, // transformation should choke
			},
		},
	}

	for _, tc := range testCases {
		s.TestInstallation.Assertions.AssertEventualCurlResponse(
			s.Ctx,
			defaults.CurlPodExecOpt,
			append(tc.opts,
				curl.WithHost(kubeutils.ServiceFQDN(proxyObjectMeta)),
				curl.WithHostHeader(fmt.Sprintf("example-%s.com", tc.routeName)),
				curl.WithPort(8080),
			),
			tc.resp)
	}
}

// TestTransformationForBodyAsString tests string body parsing transformations
func (s *testingSuite) TestTransformationForBodyAsString() {

	testCases := []struct {
		name      string
		routeName string
		opts      []curl.Option
		resp      *testmatchers.HttpResponse
	}{
		{
			name:      "dont pull info if we dont parse json", // shows we parse the body as string
			routeName: "route-for-body",
			opts: []curl.Option{
				curl.WithBody(`{"mykey": {"myinnerkey": "myinnervalue"}}`),
				curl.WithHeader("X-Incoming-Stuff", "super"),
			},
			resp: &testmatchers.HttpResponse{
				StatusCode: http.StatusBadRequest, // bad transformation results in 400
				NotHeaders: []string{
					"x-how-great",
				},
			},
		},
	}

	for _, tc := range testCases {
		s.TestInstallation.Assertions.AssertEventualCurlResponse(
			s.Ctx,
			defaults.CurlPodExecOpt,
			append(tc.opts,
				curl.WithHost(kubeutils.ServiceFQDN(proxyObjectMeta)),
				curl.WithHostHeader(fmt.Sprintf("example-%s.com", tc.routeName)),
				curl.WithPort(8080),
			),
			tc.resp)
	}
}

func (s *testingSuite) TestGatewayWithTransformedRoute() {
	// Wait for the agent gateway to be ready
	s.TestInstallation.Assertions.EventuallyGatewayCondition(
		s.Ctx,
		gateway.Name,
		gateway.Namespace,
		gwv1.GatewayConditionProgrammed,
		metav1.ConditionTrue,
	)
	s.TestInstallation.Assertions.EventuallyGatewayCondition(
		s.Ctx,
		gateway.Name,
		gateway.Namespace,
		gwv1.GatewayConditionAccepted,
		metav1.ConditionTrue,
	)

	// Wait for the agent gateway proxy pod to be running and ready before making HTTP requests
	s.TestInstallation.Assertions.EventuallyPodsRunning(s.Ctx, proxyObjectMeta.GetNamespace(), metav1.ListOptions{
		LabelSelector: fmt.Sprintf("app.kubernetes.io/name=%s", proxyObjectMeta.GetName()),
	})

	testCases := []struct {
		name      string
		routeName string
		opts      []curl.Option
		resp      *testmatchers.HttpResponse
	}{
		{
			name:      "basic-gateway-attached",
			routeName: "gateway-attached-transform",
			resp: &testmatchers.HttpResponse{
				StatusCode: http.StatusOK,
				Headers: map[string]interface{}{
					"response-gateway": "goodbye",
				},
				NotHeaders: []string{
					"x-foo-response",
				},
			},
		},
		{
			name:      "basic",
			routeName: "headers",
			opts: []curl.Option{
				curl.WithBody("hello"),
			},
			resp: &testmatchers.HttpResponse{
				StatusCode: http.StatusOK,
				Headers: map[string]interface{}{
					"x-foo-response": "notsuper",
				},
				NotHeaders: []string{
					"response-gateway",
				},
			},
		},
		{
			name:      "conditional set by request header", // inja and the request_header function in use
			routeName: "headers",
			opts: []curl.Option{
				curl.WithBody("hello"),
				curl.WithHeader("x-add-bar", "super"),
			},
			resp: &testmatchers.HttpResponse{
				StatusCode: http.StatusOK,
				Headers: map[string]interface{}{
					"x-foo-response": "supersupersuper",
				},
			},
		},
		{
			name:      "pull json info", // shows we parse the body as json
			routeName: "route-for-body-json",
			opts: []curl.Option{
				curl.WithBody(`{"mykey": {"myinnerkey": "myinnervalue"}}`),
				curl.WithHeader("X-Incoming-Stuff", "super"),
			},
			resp: &testmatchers.HttpResponse{
				StatusCode: http.StatusOK,
				Headers: map[string]interface{}{
					"x-how-great":   "level_super",
					"from-incoming": "key_level_myinnervalue",
				},
			},
		},
		{
			name:      "dont pull info if we dont parse json", // shows we parse the body as json
			routeName: "route-for-body",
			opts: []curl.Option{
				curl.WithBody(`{"mykey": {"myinnerkey": "myinnervalue"}}`),
				curl.WithHeader("X-Incoming-Stuff", "super"),
			},
			resp: &testmatchers.HttpResponse{
				StatusCode: http.StatusBadRequest, // bad transformation results in 400
				NotHeaders: []string{
					"x-how-great",
				},
			},
		},
		{
			name:      "dont pull json info if not json", // shows we parse the body as json
			routeName: "route-for-body-json",
			opts: []curl.Option{
				curl.WithBody("hello"),
			},
			resp: &testmatchers.HttpResponse{
				StatusCode: http.StatusBadRequest, // transformation should choke
			},
		},
	}
	for _, tc := range testCases {
		s.TestInstallation.Assertions.AssertEventualCurlResponse(
			s.Ctx,
			defaults.CurlPodExecOpt,
			append(tc.opts,
				curl.WithHost(kubeutils.ServiceFQDN(proxyObjectMeta)),
				curl.WithHostHeader(fmt.Sprintf("example-%s.com", tc.routeName)),
				curl.WithPort(8080),
			),
			tc.resp)
	}
}