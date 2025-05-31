job "quicktable" {
    type = "service"
    group "quicktable" {
        # Needs network named fabric
        network {
            mode = "cni/fabric"
            port "http" {
                static = 8080
                to = 8080
            }
        }
        # Needs volume named quicktable-data
        volume "quicktable-data" {
            type = "host"
            source = "quicktable-data"
        }
        task "quicktable" {
            driver = "podman"
            config {
                image = "ghcr.io/thedevtop/quicktable:latest"
                force_pull = true
                ports = ["http"]
            }
            volume_mount {
                volume = "quicktable-data"
                destination = "/data/"
            }
        }
    }
}
