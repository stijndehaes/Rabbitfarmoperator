# Rabbit farm

A small example of a kubernetes operator created using the kubebuilder project.

It has one CRD a Rabbit.farm.rabbitco.io that increases it's population every x seconds. 
It shows of how to:
- create and update another resources in kubernetes
- use requeueing to update the population in time
- how to use tests

It's a very naive implementation in the way that we don't check how much time has been progressed.
This is something that people can try to implement themselves if they understand this repo.