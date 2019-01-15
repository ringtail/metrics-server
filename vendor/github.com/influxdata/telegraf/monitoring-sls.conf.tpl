###############################################################################
#                            OUTPUT PLUGINS                                   #
###############################################################################

[[outputs.sls]]
  access_key_id = "${accessKeyId}"
  access_key_secret = "${accessKey}"
  region = "${region}"
  project = "${projectName}"
  log_store = "${logStore}"
  internal = true
  network = "${network}"
  log_level = "${logLevel}"
  verify = true
  keyPairsToConvert={"aliyun.cluster"="aliyun_cluster_id","aliyun.instance_id"="aliyun_instance_id","aliyun.service.id"="aliyun_service_id","com.docker.compose.project"="aliyun_project_id"}
