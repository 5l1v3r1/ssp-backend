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
oc create -f clusterPolicy-selfservice.yml

# Add policy to the service account
oc adm policy add-cluster-role-to-user ose:selfservice system:serviceaccount:ose-selfservice-backend:ose-selfservice

# Use the token of the service account in the container
```

Just create a 'oc new-app' from the dockerfile.

### Parameters
**Param**|**Description**|**Example**
:-----:|:-----:|:-----:
GIN\_MODE|Mode of the Webframework|debug/release
LDAP\_URL|Your LDAP|ldap.xzw.ch
LDAP\_BIND\_DN|LDAP Bind|cn=root
LDAP\_BIND\_CRED|LDAP Credentials|secret
LDAP\_SEARCH\_BASE|LDAP Search Base|ou=passport-ldapauth
LDAP\_FILTER|LDAP Filter|(uid=%s)
SESSION\_KEY|A secret password to encrypt session information|secret
OPENSHIFT\_API\_URL|Your OpenShift API Url|https://master01.ch:8443
OPENSHIFT\_TOKEN|The token from the service-account|
MAX\_QUOTA\_CPU|How many CPU can a user assign to his project|30
MAX\_QUOTA\_MEMORY|How many GB memory can a user assign to his project|50
GLUSTER\_API\_URL|The URL of your Gluster-API|http://glusterserver01:80
GLUSTER\_SECRET|The basic auth password you configured on the gluster api|secret
GLUSTER\_IPS|IP addresses of the gluster endpoints|192.168.1.1,192.168.1.2
MAX\_VOLUME\_GB|How many GB storage can a user order|100
DDC\_API|URL of the DDC Billing API|http://ddc-api.ch
AWS\_PROD\_ACCESS\_KEY\_ID|AWS Access Key ID to manage AWS ressources for production buckets|AKIAIOSFODNN7EXAMPLE
AWS\_PROD\_SECRET\_ACCESS\_KEY|AWS Secret Access Key to manage AWS ressources for production buckets|wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY
AWS\_NONPROD\_ACCESS\_KEY\_ID|AWS Access Key ID to manage AWS ressources for development buckets|AKIAIOSFODNN7EXAMPLE
AWS\_NONPROD\_SECRET\_ACCESS\_KEY|AWS Secret Access Key to manage AWS ressources for development buckets|wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY
AWS\_S3\_BUCKET\_PREFIX|Prefix for all generated S3 buckets|mycompany
AWS\_REGION|Region for all the aws artifacts|eu-central-1
SEMATEXT\_API\_TOKEN|Admin token for Sematext Logsene Apps|mytoken
SEMATEXT\_BASE\_URL|Base url for Sematext|for EU: https://apps.eu.sematext.com/
LOGSENE\_DISCOUNTCODE|Discount code for Sematext (optional)|yourcode
SEC\_API\_PASSWORD|Password for basic auth login of SEC\_API user (optional)|pass
NFS\_API\_URL|The URL of your NFS-API (optional)|https://somenfsapi.ch
NFS\_API\_SECRET|The password of the NFS-API (optional)|somesecret
NFS\_PROXY|The proxy to access the NFS-API (optional)|https://someproxy.ch:1234

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
docker build -p 8080:8080 -t ssp-backend .
# with proxy:
docker build -p 8080:8080 --build-arg https_proxy=http://proxy.ch:9000 -t ssp-backend .

# env_vars must not contain export and quotes
docker run -it --rm --env-file <(sed "s/export\s//" env_vars | tr -d "'") ssp-backend
```

There is a small script for local API testing. It handles authorization (login, token etc).
```
go run curl.go [-X GET/POST] http://localhost:8080/api/...
```
