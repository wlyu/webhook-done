/*
Copyright (c) 2019 StackRox Inc.

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

package main

import (
	"fmt"
	"k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
)

const (
	tlsDir      = `/run/secrets/tls`
	tlsCertFile = `tls.crt`
	tlsKeyFile  = `tls.key`
)

var (
	podResource = metav1.GroupVersionResource{Version: "v1", Resource: "pods"}
	labelName   = os.Getenv("LABEL_NAME")
	goreplay    = os.Getenv("GOREPLAY")
)

func addLabels(p *corev1.Pod) (patch patchOperation) {
	p.Labels[labelName] = labelName
	return patchOperation{
		Op:    "add",
		Path:  "/metadata/labels",
		Value: p.Labels,
	}
}
func applySkyWorking(req *v1.AdmissionRequest) ([]patchOperation, error) {
	if req.Resource != podResource {
		log.Printf("expect resource to be %s", podResource)
		return nil, nil
	}
	raw := req.Object.Raw
	pod := corev1.Pod{}
	if _, _, err := universalDeserializer.Decode(raw, nil, &pod); err != nil {
		return nil, fmt.Errorf("could not deserialize pod object: %v", err)
	}
	pod.Namespace = req.Namespace
	podCopy := pod.DeepCopy()
	var patches []patchOperation
	//1测试增加一个label
	if enable, ok := pod.Annotations["append.label/enabled"]; ok {
		if enable == "true" {
			patches = append(patches, addLabels(podCopy))
		}
	}
	//2测试增加一个init容器
	if enable, ok := pod.Annotations["append.goreplay/enabled"]; ok {
		if enable == "true" {
			patches = append(patches, addGoreplay(podCopy))
			patches = append(patches, addVolumnForContainers(podCopy)...)
			patches = append(patches, addInitVolumn(podCopy))
		}
	}
	return patches, nil

}

func addGoreplay(pod *corev1.Pod) patchOperation {
	req := corev1.ResourceList{
		"cpu":    resource.MustParse("10m"),
		"memory": resource.MustParse("20Mi"),
	}

	lim := corev1.ResourceList{
		"cpu":    resource.MustParse("30m"),
		"memory": resource.MustParse("50Mi"),
	}

	vault := corev1.Container{
		Name:            "goreplay-init",
		Image:           goreplay,
		ImagePullPolicy: "Never",
		Resources: corev1.ResourceRequirements{
			Requests: req,
			Limits:   lim,
		},
		VolumeMounts: []corev1.VolumeMount{
			corev1.VolumeMount{
				Name:      "goreplay-volume",
				MountPath: "/soft",
			},
		},
		Command: []string{"cp", "-rf", "/opt/goreplay/gor", "/soft/"},
	}
	pod.Spec.InitContainers = append(pod.Spec.InitContainers, vault)
	return patchOperation{
		Op:    "add",
		Path:  "/spec/initContainers",
		Value: pod.Spec.InitContainers,
	}
}
func addVolumnForContainers(p *corev1.Pod) (patchs []patchOperation) {
	containers := []patchContainers{}
	volumeMount := corev1.VolumeMount{
		Name:      "goreplay-volume",
		MountPath: "/soft",
	}
	for index, container := range p.Spec.Containers {
		container.VolumeMounts = append(container.VolumeMounts, volumeMount)
		containers = append(containers, patchContainers{
			index:        strconv.Itoa(index),
			volumeMounts: container.VolumeMounts,
			envs:         container.Env,
		})
	}
	for _, m := range containers {
		patchs = append(patchs, patchOperation{
			Op:    "add",
			Path:  "/spec/containers/" + m.index + "/volumeMounts",
			Value: m.volumeMounts,
		})
		patchs = append(patchs, patchOperation{
			Op:    "add",
			Path:  "/spec/containers/" + m.index + "/env",
			Value: m.envs,
		})
	}
	return patchs
}
func addInitVolumn(pod *corev1.Pod) patchOperation {
	volume := corev1.Volume{
		Name: "goreplay-volume",
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{
				Medium: "",
			},
		},
	}
	pod.Spec.Volumes = append(pod.Spec.Volumes, volume)
	return patchOperation{
		Op:    "add",
		Path:  "/spec/volumes",
		Value: pod.Spec.Volumes,
	}
}

func main() {
	certPath := filepath.Join(tlsDir, tlsCertFile)
	keyPath := filepath.Join(tlsDir, tlsKeyFile)
	mux := http.NewServeMux()
	mux.Handle("/api", admitFuncHandler(applySkyWorking))
	server := &http.Server{
		Addr:    ":8443",
		Handler: mux,
	}
	log.Fatal(server.ListenAndServeTLS(certPath, keyPath))
}
