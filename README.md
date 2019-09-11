# General idea
Build Status: [![Build Status](https://travis-ci.org/SchweizerischeBundesbahnen/ssp-backend.svg?branch=master)](https://travis-ci.org/SchweizerischeBundesbahnen/ssp-backend)

We at [@SchweizerischeBundesbahnen](https://github.com/SchweizerischeBundesbahnen) own a lot of projects which receive changes all the time. As those settings are (and that is fine) limited to administrative roles, we had to do a lot of manual work like:

OpenShift:
- Creating new projects with certain attributes
- Updating project metadata like billing information
- Updating project quotas
- Creating service-accounts

Persistent storage:
- Creating gluster volumes
- Increasing gluster volume sizes
- Creating PV, PVC, Gluster Service & Endpoints in OpenShift

Billing:
- Creating billing reports for different platforms

AWS:
- Creating and managing AWS S3 Buckets

Sematext:
- Creating and managing sematext logsene apps

Because of that we built this tool which allows users to execute certain tasks in self service. The tool checks permissions & multiple defined conditions.

# Components
- The Self-Service-Portal Backend (as a container)
- The Self-Service-Portal Frontend (see https://github.com/SchweizerischeBundesbahnen/cloud-selfservice-portal-frontend)
- The GlusterFS-API server (as a sytemd service)

# Installation & Documentation
## Self-Service Portal
```bash
# Create a project & a service-account
oc new-project ose-selfservice-backend
oc create serviceaccount ose-selfservice

# Add a cluster policy for the portal:
oc create -f clusterRole-selfservice.yml

# Add policy to the service account
oc adm policy add-cluster-role-to-user ose:selfservice system:serviceaccount:ose-selfservice-backend:ose-selfservice
oc adm policy add-cluster-role-to-user admin system:serviceaccount:ose-selfservice-backend:ose-selfservice

# Use the token of the service account in the container
```

Just create a 'oc new-app' from the dockerfile.

### Parameters
[openshift/ssp-backend-template.json#L254](https://github.com/SchweizerischeBundesbahnen/ssp-backend/blob/master/openshift/ssp-backend-template.json#L254)

Openshift config must be in a configmap named `config.yaml`:

```
openshift:
  - id: awsdev
    name: AWS Dev
    url: https://master.example.com:8443
    token: aeiaiesatehantehinartehinatenhiat
    glusterapi:
      url: http://glusterapi.com:2601
      secret: someverysecuresecret
      ips: 10.10.10.10, 10.10.10.11
  - id: awsprod
    name: AWS Prod
    url: https://master.example-prod.com
    token: aeiaiesatehantehinartehinatenhiat
    nfsapi:
      url: https://nfsapi.com
      secret: s3Cr3T
      proxy: http://nfsproxy.com:8000
```

### Route timeout
The `api/aws/ec2` endpoints wait until VMs have the desired state.
This can exceed the default timeout and result in a 504 error on the client.
Increasing the route timeout is described here: https://docs.openshift.org/latest/architecture/networking/routes.html#route-specific-annotations

## The GlusterFS api
Use/see the service unit file in ./glusterapi/install/

### Parameters
```bash
glusterapi -poolName=your-pool -vgName=your-vg -basePath=/your/mount -secret=yoursecret -port=yourport

# poolName = The name of the existing LV-pool that should be used to create new logical volumes
# vgName = The name of the vg where the pool lies on
# basePath = The path where the new volumes should be mounted. E.g. /gluster/mypool
# secret = The basic auth secret you specified above in the SSP
# port = The port where the server should run
# maxGB = Optinally specify max GB a volume can be. Default is 100
```

### Monitoring endpoints
The gluster api has two public endpoints for monitoring purposes. Call them this way:

The first endpoint returns usage statistics:
```bash
curl <yourserver>:<port>/volume/<volume-name>
{"totalKiloBytes":123520,"usedKiloBytes":5472}
```

The check endpoint returns if the current %-usage is below the defined threshold:
```bash

# Successful response
curl -i <yourserver>:<port>/volume/<volume-name>/check\?threshold=20
HTTP/1.1 200 OK
Content-Type: application/json; charset=utf-8
Date: Mon, 12 Jun 2017 14:23:53 GMT
Content-Length: 38

{"message":"Usage is below threshold"}

# Error response
curl -i <yourserver>:<port>/volume/<volume-name>/check\?threshold=3

HTTP/1.1 400 Bad Request
Content-Type: application/json; charset=utf-8
Date: Mon, 12 Jun 2017 14:23:37 GMT
Content-Length: 70
{"message":"Error used 4.430051813471502 is bigger than threshold: 3"}
```

For the other (internal) endpoints take a look at the code (glusterapi/main.go)

# Contributing
The backend can be started with Docker. All required environment variables must be set in the `env_vars` file.
```
# without proxy:
docker build -p 8000:8000 -t ssp-backend .
# with proxy:
docker build -p 8000:8000 --build-arg https_proxy=http://proxy.ch:9000 -t ssp-backend .

# env_vars must not contain export and quotes
docker run -it --rm --env-file <(sed "s/export\s//" env_vars | tr -d "'") ssp-backend
```

There is a small script for local API testing. It handles authorization (login, token etc).
```
go run curl.go [-X GET/POST] http://localhost:8000/api/...
```
