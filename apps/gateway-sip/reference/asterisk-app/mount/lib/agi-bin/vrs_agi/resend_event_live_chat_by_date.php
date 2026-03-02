#!/usr/bin/php -q
<?php
  require_once 'lib/func.db.php';

  // $strSQL = "SELECT * FROM `vrslog`.`log_send_popup_live_chat` WHERE `http_code` != '200' AND `finish` = 'true'";
  //$strSQL = "SELECT * FROM `vrslog`.`log_send_popup_live_chat` WHERE `http_code` != '200' AND `finish` = 'true' AND `create_at` LIKE '2022-12-05 1%'";
  //$strSQL = "SELECT * FROM `vrslog`.`log_send_popup_live_chat` WHERE `http_code` != '200' AND `finish` = 'true' AND `uniqueid` = '1677065136.497462'";
  $strSQL = "SELECT * FROM `vrslog`.`log_send_popup_live_chat` WHERE `uniqueid` IN (SELECT `uniqueid` FROM `vrslog`.`log_send_popup_live_chat` WHERE `http_code` != '200' AND `finish` = 'true' AND `create_at` LIKE '%2024-05-28%') ORDER BY `uniqueid`";
  echo $strSQL . "\n";

  $results = dbquery($strSQL, DB_FETCH_ALL);

  foreach ($results as $key => $result) {
    // condition Only sent data cannot be sent
    // $strSQL = "SELECT * FROM `vrslog`.`log_send_popup_live_chat` WHERE `uniqueid` = $result[uniqueid] AND `http_code` = '200'";
    // $res = dbquery($strSQL, DB_FETCH_ALL);
    // if (empty($res)) {
      $argv[1] = $result["event"];
      $argv[2] = $result["uniqueid"];
      $argv[3] = $result["source_number"];
      $argv[4] = $result["source_name"];
      $argv[5] = $result["destination_number"];
      $argv[6] = $result["agent_number"];
      $argv[7] = $result["agent_name"];
      $argv[8] = $result["agent_username"];
      $argv[9] = $result["source_type"];
      $argv[10] = $result["call_type"];
      $argv[11] = $result["log_type"];
      $argv[12] = $result["service_group"];
      $argv[13] = $result["card_id"];
      $argv[14] = $result["create_at"];

      // print_r($argv);

      switch ($argv[1]) {
        case 'ring':
          $event = "ring";
          break;

        case 'answer':
          $event = "answer";
          break;

        case 'agentComplete':
          $event = "hangup";
          break;

        case 'queueCallerAbandon':
          $event = "queue_caller_abandon";
          break;

        case 'agentRingNoAnswer':
          $event = "no_answer";
          break;
      }

      $value[2] = !empty($argv[2])?$argv[2]:' ';
      $value[3] = !empty($argv[3])?$argv[3]:' ';
      $value[4] = !empty($argv[4])?$argv[4]:' ';
      $value[5] = !empty($argv[5])?$argv[5]:' ';
      $value[6] = !empty($argv[6])?$argv[6]:' ';
      $value[7] = !empty($argv[7])?$argv[7]:' ';
      $value[8] = !empty($argv[8])?$argv[8]:' ';
      $value[9] = !empty($argv[9])?$argv[9]:' ';
      $value[10] = !empty($argv[10])?$argv[10]:' ';
      $value[11] = !empty($argv[11])?$argv[11]:' ';
      $value[12] = !empty($argv[12])?$argv[12]:' ';
      $value[13] = !empty($argv[13])?$argv[13]:' ';
      $value[14] = !empty($argv[14])?$argv[14]:' ';

      $keys = array(
        'uniqueid',
        'source_number',
        'source_name',
        'destination_number',
        'agent_number',
        'agent_name',
        'agent_username',
        'source_type',
        'call_type',
        'log_type',
        'service_group',
        'card_id',
        'create_at',
      );

      $payload = "";
      for ($i=0; $i < count($keys); $i++) {
        if ($payload != '') {
          $payload = $payload.'&';
        }
        $payload = $payload.$keys[$i].'='.$value[$i+2];
      }

      $domain_event_live_chat = "http://apilivechat.ttrs.or.th/receiving/";
      try {
        $curl = curl_init();

        curl_setopt_array($curl, array(
          CURLOPT_URL => $domain_event_live_chat . $event,
          CURLOPT_RETURNTRANSFER => true,
          CURLOPT_ENCODING => "",
          CURLOPT_MAXREDIRS => 10,
          CURLOPT_TIMEOUT => 0,
          CURLOPT_CONNECTTIMEOUT => 0,
          CURLOPT_FOLLOWLOCATION => true,
          CURLOPT_HTTP_VERSION => CURL_HTTP_VERSION_1_1,
          CURLOPT_CUSTOMREQUEST => "POST",
          CURLOPT_POSTFIELDS => $payload,
          CURLOPT_HTTPHEADER => array(
            "Content-Type: application/x-www-form-urlencoded"
          ),
        ));

        $response = curl_exec($curl);
        $httpCode = curl_getinfo($curl, CURLINFO_HTTP_CODE);
        echo date('h:i:s') . "\t" . "url" . "\t" . $domain_event_live_chat . $event . "\t" . $argv[2] . "\t" . $response . "\n";
        curl_close($curl);
      }
      catch(Exception $err) {
        $response = $err;
      }

      $strSQL = "UPDATE `vrslog`.`log_send_popup_live_chat`
        SET
          `http_code` = {$httpCode},
          `response` = '{$response}'
        WHERE `id` = '{$result["id"]}';";

      echo $strSQL . "\n";
      dbquery($strSQL);
    }
  return 0;
?>
