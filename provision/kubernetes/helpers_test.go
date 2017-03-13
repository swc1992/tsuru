// Copyright 2017 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package kubernetes

import (
	"errors"
	"fmt"
	"time"

	"github.com/tsuru/tsuru/provision/provisiontest"
	"gopkg.in/check.v1"
	"k8s.io/client-go/pkg/api/v1"
	batch "k8s.io/client-go/pkg/apis/batch/v1"
	extensions "k8s.io/client-go/pkg/apis/extensions/v1beta1"
)

func (s *S) TestDeploymentNameForApp(c *check.C) {
	a := provisiontest.NewFakeApp("myapp", "plat", 1)
	name := deploymentNameForApp(a, "p1")
	c.Assert(name, check.Equals, "myapp-p1")
}

func (s *S) TestDeployJobNameForApp(c *check.C) {
	a := provisiontest.NewFakeApp("myapp", "plat", 1)
	name := deployJobNameForApp(a)
	c.Assert(name, check.Equals, "myapp-deploy")
}

func (s *S) TestWaitFor(c *check.C) {
	err := waitFor(100*time.Millisecond, func() (bool, error) {
		return true, nil
	})
	c.Assert(err, check.IsNil)
	err = waitFor(100*time.Millisecond, func() (bool, error) {
		return false, nil
	})
	c.Assert(err, check.ErrorMatches, `timeout after .*`)
	err = waitFor(100*time.Millisecond, func() (bool, error) {
		return true, errors.New("myerr")
	})
	c.Assert(err, check.ErrorMatches, `myerr`)
}

func (s *S) TestWaitForJobContainerRunning(c *check.C) {
	podName, err := waitForJobContainerRunning(s.client, "job1", "cont1", 100*time.Millisecond)
	c.Assert(err, check.ErrorMatches, `timeout after .*`)
	c.Assert(podName, check.Equals, "")
	a := provisiontest.NewFakeApp("myapp", "plat", 1)
	s.client.PrependReactor("create", "jobs", s.jobWithPodReaction(a, c))
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, jErr := s.client.Batch().Jobs(tsuruNamespace).Create(&batch.Job{
			ObjectMeta: v1.ObjectMeta{
				Name:      "job1",
				Namespace: tsuruNamespace,
			},
			Spec: batch.JobSpec{
				Template: v1.PodTemplateSpec{
					ObjectMeta: v1.ObjectMeta{
						Name: "job1",
					},
					Spec: v1.PodSpec{
						Containers: []v1.Container{
							{Name: "cont1"},
						},
					},
				},
			},
		})
		c.Assert(jErr, check.IsNil)
	}()
	podName, err = waitForJobContainerRunning(s.client, "job1", "cont1", 2*time.Minute)
	c.Assert(err, check.IsNil)
	c.Assert(podName, check.Equals, "job1-pod")
	<-done
}

func (s *S) TestWaitForJob(c *check.C) {
	err := waitForJob(s.client, "job1", 100*time.Millisecond)
	c.Assert(err, check.ErrorMatches, `Job.batch "job1" not found`)
	a := provisiontest.NewFakeApp("myapp", "plat", 1)
	s.client.PrependReactor("create", "jobs", s.jobWithPodReaction(a, c))
	_, err = s.client.Batch().Jobs(tsuruNamespace).Create(&batch.Job{
		ObjectMeta: v1.ObjectMeta{
			Name:      "job1",
			Namespace: tsuruNamespace,
		},
	})
	c.Assert(err, check.IsNil)
	err = waitForJob(s.client, "job1", 2*time.Minute)
	c.Assert(err, check.IsNil)
}

func (s *S) TestCleanupPods(c *check.C) {
	for i := 0; i < 3; i++ {
		labels := map[string]string{"a": "x"}
		if i == 2 {
			labels["a"] = "y"
		}
		_, err := s.client.Core().Pods(tsuruNamespace).Create(&v1.Pod{
			ObjectMeta: v1.ObjectMeta{
				Name:      fmt.Sprintf("pod-%d", i),
				Namespace: tsuruNamespace,
				Labels:    labels,
			},
		})
		c.Assert(err, check.IsNil)
	}
	err := cleanupPods(s.client, v1.ListOptions{
		LabelSelector: "a=x",
	})
	c.Assert(err, check.IsNil)
	pods, err := s.client.Core().Pods(tsuruNamespace).List(v1.ListOptions{})
	c.Assert(err, check.IsNil)
	c.Assert(pods.Items, check.DeepEquals, []v1.Pod{{
		ObjectMeta: v1.ObjectMeta{
			Name:      "pod-2",
			Namespace: tsuruNamespace,
			Labels:    map[string]string{"a": "y"},
		},
	}})
}

func (s *S) TestCleanupJob(c *check.C) {
	a := provisiontest.NewFakeApp("myapp", "plat", 1)
	s.client.PrependReactor("create", "jobs", s.jobWithPodReaction(a, c))
	_, err := s.client.Batch().Jobs(tsuruNamespace).Create(&batch.Job{
		ObjectMeta: v1.ObjectMeta{
			Name:      "job1",
			Namespace: tsuruNamespace,
		},
		Spec: batch.JobSpec{
			Template: v1.PodTemplateSpec{
				ObjectMeta: v1.ObjectMeta{
					Name: "job1",
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{Name: "cont1"},
					},
				},
			},
		},
	})
	c.Assert(err, check.IsNil)
	err = cleanupJob(s.client, "job1")
	c.Assert(err, check.IsNil)
	pods, err := s.client.Core().Pods(tsuruNamespace).List(v1.ListOptions{})
	c.Assert(err, check.IsNil)
	c.Assert(pods.Items, check.HasLen, 0)
	jobs, err := s.client.Batch().Jobs(tsuruNamespace).List(v1.ListOptions{})
	c.Assert(err, check.IsNil)
	c.Assert(jobs.Items, check.HasLen, 0)
}

func (s *S) TestCleanupDeployment(c *check.C) {
	_, err := s.client.Extensions().Deployments(tsuruNamespace).Create(&extensions.Deployment{
		ObjectMeta: v1.ObjectMeta{
			Name:      "myapp-p1",
			Namespace: tsuruNamespace,
		},
	})
	c.Assert(err, check.IsNil)
	_, err = s.client.Extensions().ReplicaSets(tsuruNamespace).Create(&extensions.ReplicaSet{
		ObjectMeta: v1.ObjectMeta{
			Name:      "myapp-p1-xxx",
			Namespace: tsuruNamespace,
			Labels: map[string]string{
				"tsuru.app.name":    "myapp",
				"tsuru.app.process": "p1",
			},
		},
	})
	c.Assert(err, check.IsNil)
	_, err = s.client.Core().Pods(tsuruNamespace).Create(&v1.Pod{
		ObjectMeta: v1.ObjectMeta{
			Name:      "myapp-p1-xyz",
			Namespace: tsuruNamespace,
			Labels: map[string]string{
				"tsuru.app.name":    "myapp",
				"tsuru.app.process": "p1",
			},
		},
	})
	c.Assert(err, check.IsNil)
	a := provisiontest.NewFakeApp("myapp", "plat", 1)
	err = cleanupDeployment(s.client, a, "p1")
	c.Assert(err, check.IsNil)
	deps, err := s.client.Extensions().Deployments(tsuruNamespace).List(v1.ListOptions{})
	c.Assert(err, check.IsNil)
	c.Assert(deps.Items, check.HasLen, 0)
	pods, err := s.client.Core().Pods(tsuruNamespace).List(v1.ListOptions{})
	c.Assert(err, check.IsNil)
	c.Assert(pods.Items, check.HasLen, 0)
	replicas, err := s.client.Extensions().ReplicaSets(tsuruNamespace).List(v1.ListOptions{})
	c.Assert(err, check.IsNil)
	c.Assert(replicas.Items, check.HasLen, 0)
}

func (s *S) TestCleanupReplicas(c *check.C) {
	_, err := s.client.Extensions().ReplicaSets(tsuruNamespace).Create(&extensions.ReplicaSet{
		ObjectMeta: v1.ObjectMeta{
			Name:      "myapp-p1-xxx",
			Namespace: tsuruNamespace,
			Labels: map[string]string{
				"a": "x",
			},
		},
	})
	c.Assert(err, check.IsNil)
	_, err = s.client.Core().Pods(tsuruNamespace).Create(&v1.Pod{
		ObjectMeta: v1.ObjectMeta{
			Name:      "myapp-p1-xyz",
			Namespace: tsuruNamespace,
			Labels: map[string]string{
				"a": "x",
			},
		},
	})
	c.Assert(err, check.IsNil)
	err = cleanupReplicas(s.client, v1.ListOptions{
		LabelSelector: "a=x",
	})
	c.Assert(err, check.IsNil)
	deps, err := s.client.Extensions().Deployments(tsuruNamespace).List(v1.ListOptions{})
	c.Assert(err, check.IsNil)
	c.Assert(deps.Items, check.HasLen, 0)
	pods, err := s.client.Core().Pods(tsuruNamespace).List(v1.ListOptions{})
	c.Assert(err, check.IsNil)
	c.Assert(pods.Items, check.HasLen, 0)
	replicas, err := s.client.Extensions().ReplicaSets(tsuruNamespace).List(v1.ListOptions{})
	c.Assert(err, check.IsNil)
	c.Assert(replicas.Items, check.HasLen, 0)
}
