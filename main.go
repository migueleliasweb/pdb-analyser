package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"

	corev1 "k8s.io/api/core/v1"
)

// if pdbs[0].Status.DisruptionsAllowed == 0 {

func getPerContainerStatus(pod *corev1.Pod) string {

	readyContainers := 0

	for _, containerStatus := range pod.Status.ContainerStatuses {
		if containerStatus.Ready {
			readyContainers++
		}
	}

	return fmt.Sprintf("(%d/%d)", readyContainers, len(pod.Status.ContainerStatuses))
}

func main() {

	// logger := log.NewLogfmtLogger(log.NewSyncWriter(os.Stdout))
	logger := log.NewJSONLogger(log.NewSyncWriter(os.Stdout))
	logger = level.NewFilter(logger, level.AllowInfo())
	logger = log.With(logger, "ts", log.DefaultTimestampUTC, "caller", log.DefaultCaller)

	var kubeconfig string

	if home := homedir.HomeDir(); home != "" {
		kubeconfig = filepath.Join(home, ".kube", "config")
	} else {
		level.Error(logger).Log("msg", "unable to find home dir")
		os.Exit(1)
	}

	level.Debug(logger).Log("msg", fmt.Sprintf("kubeconfig=%s", kubeconfig))

	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		level.Error(logger).Log("msg", err.Error())
		os.Exit(1)
	}

	clientset, err := kubernetes.NewForConfig(config)

	if err != nil {
		level.Error(logger).Log("msg", err.Error())
		os.Exit(1)
	}

	ctx := context.Background()

	list, err := clientset.PolicyV1().PodDisruptionBudgets("").List(ctx, v1.ListOptions{})

	if err != nil {
		level.Error(logger).Log("msg", err)
		os.Exit(1)
	}

	informerFactory := informers.NewSharedInformerFactory(clientset, 0)

	podInformer := informerFactory.Core().V1().Pods()
	go podInformer.Informer().Run(ctx.Done())

	informerFactory.Start(ctx.Done())
	informerFactory.WaitForCacheSync(ctx.Done())

	for _, pdb := range list.Items {
		if pdb.Status.DisruptionsAllowed == 0 {

			selector, _ := v1.LabelSelectorAsSelector(pdb.Spec.Selector)

			pods, err := podInformer.Lister().List(selector)

			if err != nil {
				level.Error(logger).Log("msg", err)
				os.Exit(1)
			}

			if len(pods) == 0 {
				continue
			}

			fmt.Println("-----------------------------------")

			level.Info(logger).Log(
				"msg", fmt.Sprintf("pdb %s doesn't allow disruptions", pdb.Name),
				"selector", selector,
				"current-healthy", pdb.Status.CurrentHealthy,
				"min-available", pdb.Spec.MinAvailable,
			)

			for _, p := range pods {
				level.Info(logger).Log(
					"msg",
					fmt.Sprintf(
						"pod %s phase %s ready %s",
						fmt.Sprintf("%s/%s", p.Name, p.Namespace),
						p.Status.Phase,
						getPerContainerStatus(p),
					),
				)
			}
		}
	}
}
