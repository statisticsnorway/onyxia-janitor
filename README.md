# onyxia-janitor
Onyxia-Janitor is an application written in GO for managing services deployed into Onyxia/Dapla Lab

##
How it works :

Application runs in the Onyxia namespace, each job defined runs at set intervals in which it executes code.

Currently we have jobs for suspending services each night and a job for uninstalling failed service deploys.

##
Basic structure :

In cmd/main.go we use a package called "gocron" to run jobs at a schedule , example : 

```
gocron.Every(1).Day().At("20:00").Do(func()
```

Here we can also adjust the time in which the job should run and at what interval.

The job uses packages in /pkg folder , here we define the code for each job , "suspend" , "uninstall" etc

##
Adding more jobs etc. :

The aim of the app is that we can easily add more functionality if needed.

To add your code define a package with the code under /pkg , trigger the job under the main function in main.go

Create a branch , do changes , push and create PR , image is built by workflow and pushed to artifact registry.

Any Kubernetes resources required are synced by Flux to the cluster from the /config/default folder

