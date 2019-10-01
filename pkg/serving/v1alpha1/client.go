// Copyright © 2019 The Knative Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package v1alpha1

import (
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/fields"
	"knative.dev/pkg/apis"
	"knative.dev/serving/pkg/client/clientset/versioned/scheme"

	"knative.dev/client/pkg/serving"
	"knative.dev/client/pkg/util"
	"knative.dev/client/pkg/wait"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	api_serving "knative.dev/serving/pkg/apis/serving"
	"knative.dev/serving/pkg/apis/serving/v1alpha1"
	client_v1alpha1 "knative.dev/serving/pkg/client/clientset/versioned/typed/serving/v1alpha1"

	kn_errors "knative.dev/client/pkg/errors"
)

// Kn interface to serving. All methods are relative to the
// namespace specified during construction
type KnServingClient interface {

	// Get a service by its unique name
	GetService(name string) (*v1alpha1.Service, error)

	// List services
	ListServices(opts ...ListConfig) (*v1alpha1.ServiceList, error)

	// Create a new service
	CreateService(service *v1alpha1.Service) error

	// Update the given service
	UpdateService(service *v1alpha1.Service) error

	// Delete a service by name
	DeleteService(name string) error

	// Wait for a service to become ready, but not longer than provided timeout
	WaitForService(name string, timeout time.Duration) error

	// Get a configuration by name
	GetConfiguration(name string) (*v1alpha1.Configuration, error)

	// Get a revision by name
	GetRevision(name string) (*v1alpha1.Revision, error)

	// Get the "base" revision for a Service; the one that corresponds to the
	// current template.
	GetBaseRevision(service *v1alpha1.Service) (*v1alpha1.Revision, error)

	// List revisions
	ListRevisions(opts ...ListConfig) (*v1alpha1.RevisionList, error)

	// Delete a revision
	DeleteRevision(name string) error

	// Get a route by its unique name
	GetRoute(name string) (*v1alpha1.Route, error)

	// List routes
	ListRoutes(opts ...ListConfig) (*v1alpha1.RouteList, error)
}

type listConfigCollector struct {
	// Labels to filter on
	Labels labels.Set

	// Labels to filter on
	Fields fields.Set
}

// Config function for builder pattern
type ListConfig func(config *listConfigCollector)

type ListConfigs []ListConfig

// add selectors to a list options
func (opts ListConfigs) toListOptions() v1.ListOptions {
	listConfig := listConfigCollector{labels.Set{}, fields.Set{}}
	for _, f := range opts {
		f(&listConfig)
	}
	options := v1.ListOptions{}
	if len(listConfig.Fields) > 0 {
		options.FieldSelector = listConfig.Fields.String()
	}
	if len(listConfig.Labels) > 0 {
		options.LabelSelector = listConfig.Labels.String()
	}
	return options
}

// Filter list on the provided name
func WithName(name string) ListConfig {
	return func(lo *listConfigCollector) {
		lo.Fields["metadata.name"] = name
	}
}

// Filter on the service name
func WithService(service string) ListConfig {
	return func(lo *listConfigCollector) {
		lo.Labels[api_serving.ServiceLabelKey] = service
	}
}

type knServingClient struct {
	client    client_v1alpha1.ServingV1alpha1Interface
	namespace string
}

// Create a new client facade for the provided namespace
func NewKnServingClient(client client_v1alpha1.ServingV1alpha1Interface, namespace string) KnServingClient {
	return &knServingClient{
		client:    client,
		namespace: namespace,
	}
}

// Get a service by its unique name
func (cl *knServingClient) GetService(name string) (*v1alpha1.Service, error) {
	service, err := cl.client.Services(cl.namespace).Get(name, v1.GetOptions{})
	if err != nil {
		return nil, kn_errors.GetError(err)
	}
	err = updateServingGvk(service)
	if err != nil {
		return nil, err
	}
	return service, nil
}

// List services
func (cl *knServingClient) ListServices(config ...ListConfig) (*v1alpha1.ServiceList, error) {
	serviceList, err := cl.client.Services(cl.namespace).List(ListConfigs(config).toListOptions())
	if err != nil {
		return nil, kn_errors.GetError(err)
	}
	serviceListNew := serviceList.DeepCopy()
	err = updateServingGvk(serviceListNew)
	if err != nil {
		return nil, err
	}

	serviceListNew.Items = make([]v1alpha1.Service, len(serviceList.Items))
	for idx, service := range serviceList.Items {
		serviceClone := service.DeepCopy()
		err := updateServingGvk(serviceClone)
		if err != nil {
			return nil, err
		}
		serviceListNew.Items[idx] = *serviceClone
	}
	return serviceListNew, nil
}

// Create a new service
func (cl *knServingClient) CreateService(service *v1alpha1.Service) error {
	_, err := cl.client.Services(cl.namespace).Create(service)
	if err != nil {
		return kn_errors.GetError(err)
	}
	return updateServingGvk(service)
}

// Update the given service
func (cl *knServingClient) UpdateService(service *v1alpha1.Service) error {
	_, err := cl.client.Services(cl.namespace).Update(service)
	if err != nil {
		return err
	}
	return updateServingGvk(service)
}

// Delete a service by name
func (cl *knServingClient) DeleteService(serviceName string) error {
	err := cl.client.Services(cl.namespace).Delete(
		serviceName,
		&v1.DeleteOptions{},
	)
	if err != nil {
		return kn_errors.GetError(err)
	}

	return nil
}

// Wait for a service to become ready, but not longer than provided timeout
func (cl *knServingClient) WaitForService(name string, timeout time.Duration) error {
	waitForReady := newServiceWaitForReady(cl.client.Services(cl.namespace).Watch)
	return waitForReady.Wait(name, timeout)
}

// Get the configuration for a service
func (cl *knServingClient) GetConfiguration(name string) (*v1alpha1.Configuration, error) {
	configuration, err := cl.client.Configurations(cl.namespace).Get(name, v1.GetOptions{})
	if err != nil {
		return nil, err
	}
	err = updateServingGvk(configuration)
	if err != nil {
		return nil, err
	}
	return configuration, nil
}

// Get a revision by name
func (cl *knServingClient) GetRevision(name string) (*v1alpha1.Revision, error) {
	revision, err := cl.client.Revisions(cl.namespace).Get(name, v1.GetOptions{})
	if err != nil {
		return nil, kn_errors.GetError(err)
	}
	err = updateServingGvk(revision)
	if err != nil {
		return nil, err
	}
	return revision, nil
}

type NoBaseRevisionError struct {
	msg string
}

func (e NoBaseRevisionError) Error() string {
	return e.msg
}

var noBaseRevisionError = &NoBaseRevisionError{"base revision not found"}

// Get a "base" revision. This is the revision corresponding to the template of
// a Service. It may not be findable with our heuristics, in which case this
// method returns Errors()["no-base-revision"]. If it simply doesn't exist (like
// it wasn't yet created or was deleted), return the usual not found error.
func (cl *knServingClient) GetBaseRevision(service *v1alpha1.Service) (*v1alpha1.Revision, error) {
	return getBaseRevision(cl, service)
}

func getBaseRevision(cl KnServingClient, service *v1alpha1.Service) (*v1alpha1.Revision, error) {
	template, err := serving.RevisionTemplateOfService(service)
	if err != nil {
		return nil, err
	}
	// First, try to get it by name. If the template has a particular name, the
	// base revision is the one created with that name.
	if template.Name != "" {
		return cl.GetRevision(template.Name)
	}
	// Next, let's try the LatestCreatedRevision, and see if that matches the
	// template, at least in terms of the image (which is what we care about here).
	if service.Status.LatestCreatedRevisionName != "" {
		latestCreated, err := cl.GetRevision(service.Status.LatestCreatedRevisionName)
		if err != nil {
			return nil, err
		}
		latestContainer, err := serving.ContainerOfRevisionSpec(&latestCreated.Spec)
		if err != nil {
			return nil, err
		}
		container, err := serving.ContainerOfRevisionTemplate(template)
		if err != nil {
			return nil, err
		}
		if latestContainer.Image != container.Image {
			// At this point we know the latestCreatedRevision is out of date.
			return nil, noBaseRevisionError
		}
		// There is still some chance the latestCreatedRevision is out of date,
		// but we can't check the whole thing for equality because of
		// server-side defaulting. Since what we probably want it for is to
		// check the image digest anyway, keep it as good enough.
		return latestCreated, nil
	}
	return nil, noBaseRevisionError
}

// Delete a revision by name
func (cl *knServingClient) DeleteRevision(name string) error {
	err := cl.client.Revisions(cl.namespace).Delete(name, &v1.DeleteOptions{})
	if err != nil {
		return kn_errors.GetError(err)
	}

	return nil
}

// List revisions
func (cl *knServingClient) ListRevisions(config ...ListConfig) (*v1alpha1.RevisionList, error) {
	revisionList, err := cl.client.Revisions(cl.namespace).List(ListConfigs(config).toListOptions())
	if err != nil {
		return nil, kn_errors.GetError(err)
	}
	return updateServingGvkForRevisionList(revisionList)
}

// Get a route by its unique name
func (cl *knServingClient) GetRoute(name string) (*v1alpha1.Route, error) {
	route, err := cl.client.Routes(cl.namespace).Get(name, v1.GetOptions{})
	if err != nil {
		return nil, err
	}
	err = updateServingGvk(route)
	if err != nil {
		return nil, err
	}
	return route, nil
}

// List routes
func (cl *knServingClient) ListRoutes(config ...ListConfig) (*v1alpha1.RouteList, error) {
	routeList, err := cl.client.Routes(cl.namespace).List(ListConfigs(config).toListOptions())
	if err != nil {
		return nil, err
	}
	return updateServingGvkForRouteList(routeList)
}

// update all the list + all items contained in the list with
// the proper GroupVersionKind specific to Knative serving
func updateServingGvkForRevisionList(revisionList *v1alpha1.RevisionList) (*v1alpha1.RevisionList, error) {
	revisionListNew := revisionList.DeepCopy()
	err := updateServingGvk(revisionListNew)
	if err != nil {
		return nil, err
	}

	revisionListNew.Items = make([]v1alpha1.Revision, len(revisionList.Items))
	for idx := range revisionList.Items {
		revision := revisionList.Items[idx].DeepCopy()
		err := updateServingGvk(revision)
		if err != nil {
			return nil, err
		}
		revisionListNew.Items[idx] = *revision
	}
	return revisionListNew, nil
}

// update all the list + all items contained in the list with
// the proper GroupVersionKind specific to Knative serving
func updateServingGvkForRouteList(routeList *v1alpha1.RouteList) (*v1alpha1.RouteList, error) {
	routeListNew := routeList.DeepCopy()
	err := updateServingGvk(routeListNew)
	if err != nil {
		return nil, err
	}

	routeListNew.Items = make([]v1alpha1.Route, len(routeList.Items))
	for idx := range routeList.Items {
		revision := routeList.Items[idx].DeepCopy()
		err := updateServingGvk(revision)
		if err != nil {
			return nil, err
		}
		routeListNew.Items[idx] = *revision
	}
	return routeListNew, nil
}

// update with the v1alpha1 group + version
func updateServingGvk(obj runtime.Object) error {
	return util.UpdateGroupVersionKindWithScheme(obj, v1alpha1.SchemeGroupVersion, scheme.Scheme)
}

// Create wait arguments for a Knative service which can be used to wait for
// a create/update options to be finished
// Can be used by `service_create` and `service_update`, hence this extra file
func newServiceWaitForReady(watch wait.WatchFunc) wait.WaitForReady {
	return wait.NewWaitForReady(
		"service",
		watch,
		serviceConditionExtractor)
}

func serviceConditionExtractor(obj runtime.Object) (apis.Conditions, error) {
	service, ok := obj.(*v1alpha1.Service)
	if !ok {
		return nil, fmt.Errorf("%v is not a service", obj)
	}
	return apis.Conditions(service.Status.Conditions), nil
}