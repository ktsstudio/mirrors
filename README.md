# mirrors

`mirrors` is a custom Kubernetes controller operating with special [CRD](https://kubernetes.io/docs/concepts/extend-kubernetes/api-extension/custom-resources/) objects. 
Main purpose of `mirrors` currently is to copy Kubernetes Secret objects to and from various locations.

`mirrors` supports 2 kinds of source and/or destination of a secret:
1. Kubernetes Secret
2. HashiCorp Vault Secret

It means that the following scenarios are possible using `mirrors` for automatic secret mirroring:
* copy a Kubernetes Secret from one namespace to any other by a regex (e.g. copying registry credentials or TLS certificate between namespaces)
* copy a Kubernetes Secret from a namespace to HashiCorp Vault
* copy a secret from HashiCorp Vault secret to one or many Kubernetes namespaces

All these scenarios unlock possibilities to mirror any given secret to and from one or many Kubernetes clusters.

## CRD Overview

`SecretMirror` available fields are documented [here](https://doc.crds.dev/github.com/ktsstudio/mirrors/mirrors.kts.studio/SecretMirror/v1alpha2). 

## Quick example

Let's say we have a following Kubernetes secret:
```yaml
apiVersion: v1
kind: Secret
metadata:
  name: mysecret
type: Opaque
stringData:
  username: hellothere
  password: generalkenobi
```

It is deployed to a namespace `default` within a Kubernetes cluster. Using the following `SecretMirror` object one can easily replicate this secret to any namespace which matches the regular expression `demo-namespace-\d+`:
```yaml
apiVersion: mirrors.kts.studio/v1alpha2
kind: SecretMirror
metadata:
  name: mysecret
spec:
  source:
    name: mysecret
  destination:
    namespaces:
      - demo-namespace-\d+
```

After deploying this `SecretMirror` a Secret named `mysecret` is going to 
be copied to all the destination namespaces and will do this continuously 
once every 3 minutes (this can be configured with a `pollPeriodSeconds` setting in each `SecretMirror`).

`mirrors` was specifically designed so that it's not monitoring changes in Secret 
objects in order to avoid stressing Kubernetes API with a lot of requests 
and updates (because usually there are a lot of Secrets in a 
typical Kubernetes cluster).

As you can see `destination.namespaces` is an array, so it is possible to 
specify multiple regexps. Secret will be copied to all the matched namespaces.

_**Security note:** `mirrors` specifically does not allow copying a secret from arbitrary namespace, only from the 
namespace where a SecretMirror is deployed._ 


## Vault examples

### Copy to Vault

You can easily replicate a secret `mysecret` to your HashiCorp Vault cluster by using the following spec:
```yaml
apiVersion: mirrors.kts.studio/v1alpha2
kind: SecretMirror
metadata:
  name: mysecret
spec:
  source:
    name: mysecret
  destination:
    type: vault
    vault:
      addr: https://vault.example.com
      path: /secret/data/myteam/mysecret
      auth:
        approle:
          secretRef:
            name: vault-approle
```

This `SecretMirror` will use an AppRole auth mechanism in order to login to Vault and obtain a token.
There should exist a `vault-approle` secret containing appRole credentials:
```yaml
apiVersion: v1
kind: Secret
metadata:
  name: vault-approle
type: Opaque
stringData:
  role-id: your-role-id
  secret-id: your-secret-id
```

It is also possible to authenticate using a VAULT_TOKEN directly:
```yaml
apiVersion: mirrors.kts.studio/v1alpha2
kind: SecretMirror
metadata:
  name: mysecret
spec:
  source:
    name: mysecret
  destination:
    type: vault
    vault:
      addr: https://vault.example.com
      path: /secret/data/myteam/mysecret
      auth:
        token:
          secretRef:
            name: vault-token
```

with the secret, containing vault token:
```yaml
apiVersion: v1
kind: Secret
metadata:
  name: vault-token
  namespace: default
type: Opaque
stringData:
  token: s.YOURTOKEN
```

**But this is highly discouraged, because currently there is no token renewal 
mechanism in `mirrors` so if your token will expire `mirrors` can do nothing 
with that, and you will be forced to update a token in the secret.**


### Copy from Vault
In order to copy a Secret from HashiCorp Vault to Kubernetes use the following `SecretMirror`:
```yaml
apiVersion: mirrors.kts.studio/v1alpha2
kind: SecretMirror
metadata:
  name: mysecret2
spec:
  source:
    name: mysecret2
    type: vault
    vault:
      addr: https://vault.example.com
      path: /secret/data/myteam/mysecret
      auth:
        approle:
          secretRef:
            name: vault-approle
  destination:
    namespaces:
      - important-namespace-\d+
```

It is required to specify `source.name` as it will be the future name of Kuberentes secrets created in the cluster.

## More examples

More examples can be found at `config/samples` folder.

## Install

In order to install `mirrors` controller you need to execute the following:

```shell
helm repo add kts https://charts.kts.studio
helm repo update

helm upgrade --install mirrors kts/mirrors
```

Or using built-in kustomize deployment:
```bash
git clone https://github.com/ktsstudio/mirrors
cd mirrors
cd config/default
kustomize edit set image controller=ktshub/mirrors:0.2.3
kustomize build . | kubectl apply -f -
```

## Exposed metrics

The following specific mirrors metrics are exposed:

| metric                                  | description                                                           |
|-----------------------------------------|-----------------------------------------------------------------------|
| `mirrors_sync_total`                    | Number of successful mirror syncs                                     |
| `mirrors_ns_current_count`              | Number of namespaces to which a secret has been successfully mirrored |
| `mirrors_vault_lease_renew_ok_total`    | Number of successful lease renewals                                   |
| `mirrors_vault_lease_renew_error_total` | Number of errored lease renewals                                      |

