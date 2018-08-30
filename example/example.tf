provider "aws" {
    access_key = "${var.access_key}"
    secret_key = "${var.secret_key}"
    region = "${var.region}"
}

resource "aws_instance" "puppetmaster" {
    ami = "${var.aws_ami_id}"
    instance_type = "t2.medium"
    key_name = "${var.aws_key_pair}"

    connection {
        type = "ssh"
        user = "ubuntu"
        private_key = "${file("~/.ssh/id_rsa")}"
    }

    timeouts {
        create = "15m"
    }

    provisioner "file" {
        source = "pe.conf"
        destination = "/tmp/pe.conf"
    }

    provisioner "file" {
        source = "autosign-batch.json"
        destination = "/tmp/autosign-batch.json"
    }

    provisioner "remote-exec" {
        on_failure = "continue"
        inline = [
            "curl -L -o /tmp/puppet-enterprise-${var.pe_version}-${var.pe_platform}.tar.gz https://s3.amazonaws.com/pe-builds/released/${var.pe_version}/puppet-enterprise-${var.pe_version}-${var.pe_platform}.tar.gz",
            "tar zxf /tmp/puppet-enterprise-${var.pe_version}-${var.pe_platform}.tar.gz -C /tmp",
            "sudo mkdir -p /etc/puppetlabs/puppet",
            "sudo /tmp/puppet-enterprise-${var.pe_version}-${var.pe_platform}/puppet-enterprise-installer -c /tmp/pe.conf",
            "sleep 120",
            "sudo puppet agent -t",
            "sudo puppet module install danieldreier/autosign",
            "sudo /opt/puppetlabs/puppet/bin/gem install ncedit",
            "sudo /opt/puppetlabs/puppet/bin/ncedit update_classes",
            "sudo /opt/puppetlabs/puppet/bin/ncedit batch --json-file /tmp/autosign-batch.json",
        ]
    }
}

resource "aws_instance" "agent" {
    ami = "${var.aws_ami_id}"
    instance_type = "t2.medium"
    key_name = "${var.aws_key_pair}"

    connection {
        type = "ssh"
        user = "ubuntu"
        private_key = "${file("~/.ssh/id_rsa")}"
    }

    provisioner "puppet" {
        use_sudo = true
        server = "${aws_instance.puppetmaster.public_dns}"
    }
}

resource "aws_instance" "os_agent" {
    ami = "${var.aws_ami_id}"
    instance_type = "t2.medium"
    key_name = "${var.aws_key_pair}"

    connection {
        type = "ssh"
        user = "ubuntu"
        private_key = "${file("~/.ssh/id_rsa")}"
    }

    provisioner "puppet" {
        use_sudo = true
        open_source = true
        server = "${aws_instance.puppetmaster.public_dns}"
    }
}
