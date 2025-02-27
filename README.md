# onyxia-janitor
Onyxia-Janitor is an application written in GO for managing services deployed into Onyxia/Dapla Lab

##
How it works :

Onyxia Janitor is triggered based on Cronjobs placed in the cluster folders in dapla-lab-iac repo : https://github.com/statisticsnorway/dapla-lab-iac/tree/main/clusters

Each Cronjob is structured so the command you want to run is set as environment value, example : 

            env:
            - name: ONYXIA_JANITOR_ACTION
              value: suspend

The cronjob also has values for which helm releases it will affect etc.

Once the cronjob schedule is triggered it will start a job that starts a pod with the Onyxia-Janitor image and performs the specified job.

##
Basic structure :

main.go , app starts when Cronjob triggers a job:

Parses env variables , starts "case" based on value of ONYXIA_JANITOR_ACTION.

Suspend, Notify, Uninstall is separated into separate pkg folders.

##
Adding more jobs etc. :

The aim of the app is that we can easily add more functionality if needed.

To add your code define a package with the code under /pkg , trigger the job under the main function in main.go

Create a branch , do changes , push and create PR , image is built by workflow and pushed to artifact registry.

Any Kubernetes resources required are synced by Flux to the cluster from the /config/default folder

