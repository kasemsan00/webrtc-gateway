<?php
  require_once 'lib/func.db.php';

  $strSQL = "SELECT * FROM `vrslog`.`log_send_popup` WHERE `http_code` != '200' AND `finish` = 'true';";
  $results = dbquery($strSQL, DB_FETCH_ALL);

  foreach ($results as $key => $result) {
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

    try {
      $curl = curl_init();
  
      curl_setopt_array($curl, array(
        CURLOPT_URL => "http://203.150.245.41:4400/".$event,
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
      echo date('h:i:s') . "\t" . $response . "\n";
      $httpCode = curl_getinfo($curl, CURLINFO_HTTP_CODE);
      curl_close($curl);
    }
    catch(Exception $response) {
      echo date('h:i:s') . $response . "\n";
    }

    $strSQL = "UPDATE `vrslog`.`log_send_popup`
      SET
          `http_code` = {$httpCode},
          `response` = '{$response}'
      WHERE `uniqueid` = '{$argv[2]}';";

    dbquery($strSQL);

  }
  return 0;
?>
