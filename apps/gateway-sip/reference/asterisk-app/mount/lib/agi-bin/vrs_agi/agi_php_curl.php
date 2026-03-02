#!/usr/bin/php -q
<?php
  require_once 'env_for_agi.php';
  require_once 'lib/phpagi.php';
  require_once 'lib/func.db.php';

  $agi = new AGI();

  $agi->verbose("----------------------------------------------------------------------------");
  $agi->verbose($agi->get_variable("NODEST")[data]);
  if ($queue_for_icrm[$agi->get_variable("NODEST")[data]] == 1) {

    if ($argv[1] == 'ring')
      $event = "callRing";
    elseif ($argv[1] == 'answer')
      $event = "callAnswer";
    elseif ($argv[1] == 'agentComplete')
      $event = "callHangup";
    elseif ($argv[1] == 'queueCallerAbandon')
      $event = "callAbandon";
    elseif ($argv[1] == 'agentRingNoAnswer')
      $event = "callNoanswer";

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
    );

    $strData = "";
    $httpCode = "";

    for ($i=0; $i < count($keys); $i++) {
      if ($strData != '')
        $strData = $strData.'&';

      $strData = $strData.$keys[$i].'='.$value[$i+2];
    }

    $agi->verbose($strData);

    try {
      $curl = curl_init();

      curl_setopt_array($curl, array(
        CURLOPT_URL => $url_popup."/".$event,
        CURLOPT_RETURNTRANSFER => true,
        CURLOPT_ENCODING => "",
        CURLOPT_MAXREDIRS => 10,
        CURLOPT_TIMEOUT => 3,
        CURLOPT_CONNECTTIMEOUT => 1,
        CURLOPT_FOLLOWLOCATION => true,
        CURLOPT_HTTP_VERSION => CURL_HTTP_VERSION_1_1,
        CURLOPT_CUSTOMREQUEST => "POST",
        CURLOPT_POSTFIELDS => $strData,
        CURLOPT_HTTPHEADER => array(
          "Content-Type: application/x-www-form-urlencoded"
        ),
      ));

      $response = curl_exec($curl);
      $agi->verbose($response);
      $httpCode = curl_getinfo($curl, CURLINFO_HTTP_CODE);
      curl_close($curl);
    }
    catch(Exception $response) {
      $agi->verbose($response);
    }

    date_default_timezone_set('Asia/Bangkok');
    $strData = "";
    $strData = $strData . date('Y-m-d H:i:s') . ' ';
    for ($i=0; $i < count($keys); $i++) {
      if ($i > 0)
        $strData = $strData . ', '; 
      
      $strData = $strData . $keys[$i] . ' = ' . $value[$i+2];
    }
    $strData = $strData . ', ' . "http code" . ' = ' . $httpCode . ', ' . "event" . ' = ' . $argv[1] . ', ' . $response;
    error_log($strData."\n", 3, "/var/log/asterisk/log_send_popup.log");

    if ($argv[1] == agentComplete || $argv[1] == queueCallerAbandon)
      $finish = 'true';
    else
      $finish = 'false';

    $value[2] = !empty($argv[2])?"'$argv[2]'":'NULL';
    $value[3] = !empty($argv[3])?"'$argv[3]'":'NULL';
    $value[4] = !empty($argv[4])?"'$argv[4]'":'NULL';
    $value[5] = !empty($argv[5])?"'$argv[5]'":'NULL';
    $value[6] = !empty($argv[6])?"'$argv[6]'":'NULL';
    $value[7] = !empty($argv[7])?"'$argv[7]'":'NULL';
    $value[8] = !empty($argv[8])?"'$argv[8]'":'NULL';
    $value[9] = !empty($argv[9])?"'$argv[9]'":'NULL';
    $value[10] = !empty($argv[10])?"'$argv[10]'":'NULL';
    $value[11] = !empty($argv[11])?"'$argv[11]'":'NULL';
    $value[12] = !empty($argv[12])?"'$argv[12]'":'NULL';
    $value[13] = !empty($argv[13])?"'$argv[13]'":'NULL';

    $strSQL = "INSERT INTO `vrslog`.`log_send_popup`(
      `uniqueid`,
      `source_number`,
      `source_name`,
      `destination_number`,
      `agent_number`,
      `agent_name`,
      `agent_username`,
      `source_type`,
      `call_type`,
      `log_type`,
      `service_group`,
      `card_id`,
      `event`,
      `http_code`,
      `response`,
      `finish`
    ) VALUES(
      {$value[2]},
      {$value[3]},
      {$value[4]},
      {$value[5]},
      {$value[6]},
      {$value[7]},
      {$value[8]},
      {$value[9]},
      {$value[10]},
      {$value[11]},
      {$value[12]},
      {$value[13]},
      '{$argv[1]}',
      '{$httpCode}',
      '{$response}',
      '{$finish}'
    )";

    dbquery($strSQL);
  }
  $agi->verbose("----------------------------------------------------------------------------");
  return 0;
?>
