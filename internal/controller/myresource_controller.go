/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"fmt"
	"io"
	"net/http"

	//"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	appv1 "github.com/MarvinPetzoldt/multi-deployment/api/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

// MyResourceReconciler reconciles a MyResource object
type MyResourceReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Clientset *kubernetes.Clientset
}

// GetNodeCPUInfo retrieves a map of nodeID to their CPU capacities, i.e. number of available CPUs per node.
func (r *MyResourceReconciler) GetNodeCPUInfo() (map[string]int64, error) {
	nodes, err := r.Clientset.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	

	nodeCPUInfo := make(map[string]int64)
	for _, node := range nodes.Items {
		cpuQuantity := node.Status.Capacity[corev1.ResourceCPU]
		cpuCount, _ := cpuQuantity.AsInt64() // CPU capacity is expressed in "cores"
		nodeCPUInfo[node.Name] = cpuCount
	}

	return nodeCPUInfo, nil
}


func GenerateCoreNaming(nodeCPUInfo map[string]int64) map[string][]string {

	computeNodes := make(map[string][]string)

	for nodeName, cpuCount := range nodeCPUInfo {
		var cores []string
		for i := int64(0); i < cpuCount; i++ {
			coreID := fmt.Sprintf("%s-core-%d", nodeName, i+1)
			cores = append(cores, coreID)
		}
		computeNodes[nodeName] = cores
	}

	return computeNodes
}


//+kubebuilder:rbac:groups=app.example.com,resources=myresources,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=app.example.com,resources=myresources/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=app.example.com,resources=myresources/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the MyResource object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.17.3/pkg/reconcile
func (r *MyResourceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	log.Info("Reconciling...")
	// TODO(user): your logic here
	myResource := &appv1.MyResource{}
	err := r.Get(ctx, req.NamespacedName, myResource)
	if err != nil {
		if client.IgnoreNotFound(err) == nil {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	nodeCpuInfo, err := r.GetNodeCPUInfo()
	if err != nil {
		log.Error(err, "Unable to retrieve the NodeCPUInfo.")
	}

	for nodeName, cpuCount := range nodeCpuInfo {
		log.Info("Node CPU Info", "Node", nodeName, "CPU Cores", cpuCount)
	}

	coreNaming := GenerateCoreNaming(nodeCpuInfo)
	for computeNodeName, coreName := range coreNaming {
		log.Info("CoreNaming", "NodeName", computeNodeName, "CoreName", coreName)
	}


	// Example HTTP GET request to the simple service
    serviceURL := "http://simple-service.default.svc.cluster.local"
    resp, err := http.Get(serviceURL)
    if err != nil {
        log.Error(err, "Failed to make HTTP request to simple service")
        return ctrl.Result{}, err
    }
    defer resp.Body.Close()

    body, err := io.ReadAll(resp.Body)
    if err != nil {
        log.Error(err, "Failed to read response body from simple service")
        return ctrl.Result{}, err
    }

    message := string(body)
    log.Info("Received response from simple service", "message", message)
	
	// Define Deployments based on the names in MyResource
	for _, name := range myResource.Spec.Names {
		deployment := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: myResource.Namespace,
			},
			Spec: appsv1.DeploymentSpec{
				Replicas: int32Ptr(1),
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"app": name},
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{"app": name},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  name,
								Image: "nginx", // Replace with your image
								Ports: []corev1.ContainerPort{
									{
										ContainerPort: 80,
										Name:          "http",
										Protocol:      corev1.ProtocolTCP,
									},
								},
							},
						},
					},
				},
			},
		}

		// Set MyResource instance as the owner and controller
		if err := controllerutil.SetControllerReference(myResource, deployment, r.Scheme); err != nil {
			return ctrl.Result{}, err
		}

		// Check if the Deployment already exists
		found := &appsv1.Deployment{}
		err := r.Get(ctx, types.NamespacedName{Name: deployment.Name, Namespace: deployment.Namespace}, found)
		if err != nil && errors.IsNotFound(err) {
			// Create the Deployment
			err = r.Create(ctx, deployment)
			if err != nil {
				return ctrl.Result{}, err
			}
		} else if err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}



// SetupWithManager sets up the controller with the Manager.
func (r *MyResourceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&appv1.MyResource{}).
		Owns(&appsv1.Deployment{}).
		Complete(r)
}

func int32Ptr(i int32) *int32 { return &i }