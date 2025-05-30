job "quicktable" {
    type = "service"
    group "quicktable" {
        task "quicktable" {
            driver = "docker"
            config {
                image = "ghcr.io/thedevtop/quicktable:latest"
                init = true
                network_mode = "bridge"
                volumes = [
                    "quicktable_data:/data"
                ]
            }
        }
    }
}
