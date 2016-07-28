package transformer

import (
	"github.com/skippbox/kompose/pkg/kobject"
	"fmt"
	"os"
)

func Transform(komposeObject *kobject.KomposeObject, opt kobject.ConvertOptions) (map[string][]byte, map[string][]byte, map[string][]byte, map[string][]byte, map[string][]byte, []string) {
	mServices := make(map[string][]byte)
	mReplicationControllers := make(map[string][]byte)
	mDeployments := make(map[string][]byte)
	mDaemonSets := make(map[string][]byte)
	mReplicaSets := make(map[string][]byte)
	// OpenShift DeploymentConfigs
	mDeploymentConfigs := make(map[string][]byte)

	f := createOutFile(opt.outFile)
	defer f.Close()

	var svcnames []string

	for name, service := range komposeObject.ServiceConfigs {
		svcnames = append(svcnames, name)

		rc := initRC(name, service, opt.replicas)
		sc := initSC(name, service)
		dc := initDC(name, service, opt.replicas)
		ds := initDS(name, service)
		osDC := initDeploymentConfig(name, service, opt.replicas) // OpenShift DeploymentConfigs

		// Configure the environment variables.
		envs := configEnvs(name, service)

		// Configure the container command.
		var cmds []string
		for _, cmd := range service.Command {
			cmds = append(cmds, cmd)
		}
		// Configure the container volumes.
		volumesMount, volumes := configVolumes(service)

		// Configure the container ports.
		ports := configPorts(name, service)

		// Configure the service ports.
		servicePorts := configServicePorts(name, service)
		sc.Spec.Ports = servicePorts

		// Configure label
		labels := map[string]string{"service": name}
		sc.ObjectMeta.Labels = labels
		// Configure annotations
		annotations := map[string]string{}
		for key, value := range service.Annotations {
			annotations[key] = value
		}
		sc.ObjectMeta.Annotations = annotations

		// fillTemplate fills the pod template with the value calculated from config
		fillTemplate := func(template *api.PodTemplateSpec) {
			template.Spec.Containers[0].Env = envs
			template.Spec.Containers[0].Command = cmds
			template.Spec.Containers[0].WorkingDir = service.WorkingDir
			template.Spec.Containers[0].VolumeMounts = volumesMount
			template.Spec.Volumes = volumes
			// Configure the container privileged mode
			if service.Privileged == true {
				template.Spec.Containers[0].SecurityContext = &api.SecurityContext{
					Privileged: &service.Privileged,
				}
			}
			template.Spec.Containers[0].Ports = ports
			template.ObjectMeta.Labels = labels
			// Configure the container restart policy.
			switch service.Restart {
			case "", "always":
				template.Spec.RestartPolicy = api.RestartPolicyAlways
			case "no":
				template.Spec.RestartPolicy = api.RestartPolicyNever
			case "on-failure":
				template.Spec.RestartPolicy = api.RestartPolicyOnFailure
			default:
				logrus.Fatalf("Unknown restart policy %s for service %s", service.Restart, name)
			}
		}

		// fillObjectMeta fills the metadata with the value calculated from config
		fillObjectMeta := func(meta *api.ObjectMeta) {
			meta.Labels = labels
			meta.Annotations = annotations
		}

		// Update each supported controllers
		updateController(rc, fillTemplate, fillObjectMeta)
		updateController(dc, fillTemplate, fillObjectMeta)
		updateController(ds, fillTemplate, fillObjectMeta)
		// OpenShift DeploymentConfigs
		updateController(osDC, fillTemplate, fillObjectMeta)

		// convert datarc to json / yaml
		datarc, err := transformer(rc, opt.generateYaml)
		if err != nil {
			logrus.Fatalf(err.Error())
		}

		// convert datadc to json / yaml
		datadc, err := transformer(dc, opt.generateYaml)
		if err != nil {
			logrus.Fatalf(err.Error())
		}

		// convert datads to json / yaml
		datads, err := transformer(ds, opt.generateYaml)
		if err != nil {
			logrus.Fatalf(err.Error())
		}

		var datasvc []byte
		// If ports not provided in configuration we will not make service
		if len(ports) == 0 {
			logrus.Warningf("[%s] Service cannot be created because of missing port.", name)
		} else if len(ports) != 0 {
			// convert datasvc to json / yaml
			datasvc, err = transformer(sc, opt.generateYaml)
			if err != nil {
				logrus.Fatalf(err.Error())
			}
		}

		// convert OpenShift DeploymentConfig to json / yaml
		dataDeploymentConfig, err := transformer(osDC, opt.generateYaml)
		if err != nil {
			logrus.Fatalf(err.Error())
		}

		mServices[name] = datasvc
		mReplicationControllers[name] = datarc
		mDeployments[name] = datadc
		mDaemonSets[name] = datads
		mDeploymentConfigs[name] = dataDeploymentConfig
	}

	return mServices, mDeployments, mDaemonSets, mReplicationControllers, mDeploymentConfigs, svcnames
}

func PrintControllers(mServices, mDeployments, mDaemonSets, mReplicationControllers, mDeploymentConfigs map[string][]byte, svcnames []string, opt convertOptions, f *os.File) {
	for k, v := range mServices {
		if v != nil {
			print(k, "svc", v, opt.toStdout, opt.generateYaml, f)
		}
	}

	// If --out or --stdout is set, the validation should already prevent multiple controllers being generated
	if opt.createD {
		for k, v := range mDeployments {
			print(k, "deployment", v, opt.toStdout, opt.generateYaml, f)
		}
	}

	if opt.createDS {
		for k, v := range mDaemonSets {
			print(k, "daemonset", v, opt.toStdout, opt.generateYaml, f)
		}
	}

	if opt.createRC {
		for k, v := range mReplicationControllers {
			print(k, "rc", v, opt.toStdout, opt.generateYaml, f)
		}
	}

	if f != nil {
		fmt.Fprintf(os.Stdout, "file %q created\n", opt.outFile)
	}

	if opt.createChart {
		err := generateHelm(opt.inputFile, svcnames, opt.generateYaml, opt.createD, opt.createDS, opt.createRC, opt.outFile)
		if err != nil {
			logrus.Fatalf("Failed to create Chart data: %v", err)
		}
	}

	if opt.createDeploymentConfig {
		for k, v := range mDeploymentConfigs {
			print(k, "deploymentconfig", v, opt.toStdout, opt.generateYaml, f)
		}
	}
}