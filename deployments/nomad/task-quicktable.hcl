job "quicktable" {
    type = "service"
    group "quicktable" {
        # Needs network named fabric
        network {
            mode = "cni/fabric"
            port "http" {
                to = 8080
            }
        }
        # Needs volume named quicktable-data
        volume "quicktable-data" {
            type = "host"
            source = "quicktable-data"
        }
        task "quicktable" {
            driver = "docker"
            config {
                image = "ghcr.io/thedevtop/quicktable:latest"
                ports = ["http"]
            }
            volume_mount {
                volume = "quicktable-data"
                destination = "/data/"
            }
        }
    }
}
