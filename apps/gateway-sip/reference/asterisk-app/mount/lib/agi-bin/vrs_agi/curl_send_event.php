#!/usr/bin/php -q
<?php
  require_once 'env_for_agi.php';
  require_once 'lib/phpagi.php';
  require_once 'lib/func.db.php';


  $agi = new AGI();
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

  $event = GetEventFromAsterisk($agi);
    
  if ($queue_for_icrm[$event[queue]] == 1) {
    $e = SetEvent($event);
    $payload = SetDataForXWwwFormUrlencoded($keys, $e);
    $response = CurlEvent($domain_event, $e[route], $payload);
    VerboseEvent($agi, $event, $response[response]);
    WriteErrorLog($keys, $e, $response[httpCode], $event[event], $response[response]);
    InsertEventToSql($event, $response[httpCode], $response[response]);
  }
  
  return 0;


  function GetEventFromAsterisk($agi) {
    $event[event] = $agi->get_variable("event")["data"];
    $event[uniqueid] = $agi->get_variable("uniqueid")["data"];
    $event[source_number] = $agi->get_variable("source_number")["data"];
    $event[source_name] = $agi->get_variable("source_name")["data"];
    $event[destination_number] = $agi->get_variable("destination_number")["data"];
    $event[agent_number] = $agi->get_variable("agent_number")["data"];
    $event[agent_name] = $agi->get_variable("agent_name")["data"];
    $event[agent_username] = $agi->get_variable("agent_username")["data"];
    $event[source_type] = $agi->get_variable("source_type")["data"];
    $event[call_type] = $agi->get_variable("call_type")["data"];
    $event[log_type] = $agi->get_variable("log_type")["data"];
    $event[service_group] = $agi->get_variable("service_group")["data"];
    $event[card_id] = $agi->get_variable("card_id")["data"];
    $event[queue] = $agi->get_variable("NODEST")["data"];
    return $event;
  }

  function SetEvent($event) {
    if ($event[event] == 'ring')
      $value[route] = "callRing";
    elseif ($event[event] == 'answer')
      $value[route] = "callAnswer";
    elseif ($event[event] == 'agentComplete')
      $value[route] = "callHangup";
    elseif ($event[event] == 'queueCallerAbandon')
      $value[route] = "callAbandon";
    elseif ($event[event] == 'agentRingNoAnswer')
      $value[route] = "callNoanswer";

    $value[2] = !empty($event[uniqueid])?$event[uniqueid]:' ';
    $value[3] = !empty($event[source_number])?$event[source_number]:' ';
    $value[4] = !empty($event[source_name])?$event[source_name]:' ';
    $value[5] = !empty($event[destination_number])?$event[destination_number]:' ';
    $value[6] = !empty($event[agent_number])?$event[agent_number]:' ';
    $value[7] = !empty($event[agent_name])?$event[agent_name]:' ';
    $value[8] = !empty($event[agent_username])?$event[agent_username]:' ';
    $value[9] = !empty($event[source_type])?$event[source_type]:' ';
    $value[10] = !empty($event[call_type])?$event[call_type]:' ';
    $value[11] = !empty($event[log_type])?$event[log_type]:' ';
    $value[12] = !empty($event[service_group])?$event[service_group]:' ';
    $value[13] = !empty($event[card_id])?$event[card_id]:' ';
    return $value;
  }

  function CurlEvent($domain, $route, $payload) {
    try {
      $curl = curl_init();

      curl_setopt_array($curl, array(
        CURLOPT_URL => $domain . $route,
        CURLOPT_RETURNTRANSFER => true,
        CURLOPT_ENCODING => "",
        CURLOPT_MAXREDIRS => 10,
        CURLOPT_TIMEOUT => 3,
        CURLOPT_CONNECTTIMEOUT => 1,
        CURLOPT_FOLLOWLOCATION => true,
        CURLOPT_HTTP_VERSION => CURL_HTTP_VERSION_1_1,
        CURLOPT_CUSTOMREQUEST => "POST",
        CURLOPT_POSTFIELDS => $payload,
        CURLOPT_HTTPHEADER => array(
          "Content-Type: application/x-www-form-urlencoded"
        ),
      ));

      $response[domain] = $domain;
      $response[route] = $route;
      $response[response] = curl_exec($curl);
      $response[httpCode] = curl_getinfo($curl, CURLINFO_HTTP_CODE);
      curl_close($curl);
      return $response;
    }
    catch(Exception $err) {
      $response[response] = $err;
      return $response;
    }
  }

  function SetDataForXWwwFormUrlencoded($keys, $value) {
    $payload = "";

    for ($i=0; $i < count($keys); $i++) {
      if ($payload != '')
        $payload = $payload . '&';

      $payload = $payload . $keys[$i] . '=' . $value[$i+2];
    }

    return $payload;
  }

  function VerboseEvent($agi, $event, $response) {
    $agi->verbose("----------------------------------------------------------------------------------------------");
    $agi->verbose("queue                " . $event[queue]);
    $agi->verbose("event                " . $event[event]);
    $agi->verbose("uniqueid             " . $event[uniqueid]);
    $agi->verbose("source_number        " . $event[source_number]);
    $agi->verbose("source_name          " . $event[source_name]);
    $agi->verbose("destination_number   " . $event[destination_number]);
    $agi->verbose("agent_number         " . $event[agent_number]);
    $agi->verbose("agent_name           " . $event[agent_name]);
    $agi->verbose("agent_username       " . $event[agent_username]);
    $agi->verbose("source_type          " . $event[source_type]);
    $agi->verbose("call_type            " . $event[call_type]);
    $agi->verbose("log_type             " . $event[log_type]);
    $agi->verbose("service_group        " . $event[service_group]);
    $agi->verbose("card_id              " . $event[card_id]);
    $agi->verbose("----------------------------------------------------------------------------------------------");
    $agi->verbose($response);
    $agi->verbose("----------------------------------------------------------------------------------------------");
  }
  
  function WriteErrorLog($keys, $value, $httpCode, $event, $response) {
    date_default_timezone_set('Asia/Bangkok');
    $payload = "";
    $payload = $payload . date('Y-m-d H:i:s') . ' ';
    for ($i=0; $i < count($keys); $i++) {
      if ($i > 0)
        $payload = $payload . ', '; 
      
      $payload = $payload . $keys[$i] . ' = ' . $value[$i+2];
    }
    $payload = $payload . ', ' . "http code" . ' = ' . $httpCode . ', ' . "event" . ' = ' . $event . ', ' . $response;
    error_log($payload."\n", 3, "/var/log/asterisk/log_send_event.log");
  }

  function InsertEventToSql($event, $httpCode, $response) {
    if ($event[event] == agentComplete || $event[event] == queueCallerAbandon)
      $finish = 'true';
    else
      $finish = 'false';

    $value[2] = !empty($event[uniqueid])?"'$event[uniqueid]'":'NULL';
    $value[3] = !empty($event[source_number])?"'$event[source_number]'":'NULL';
    $value[4] = !empty($event[source_name])?"'$event[source_name]'":'NULL';
    $value[5] = !empty($event[destination_number])?"'$event[destination_number]'":'NULL';
    $value[6] = !empty($event[agent_number])?"'$event[agent_number]'":'NULL';
    $value[7] = !empty($event[agent_name])?"'$event[agent_name]'":'NULL';
    $value[8] = !empty($event[agent_username])?"'$event[agent_username]'":'NULL';
    $value[9] = !empty($event[source_type])?"'$event[source_type]'":'NULL';
    $value[10] = !empty($event[call_type])?"'$event[call_type]'":'NULL';
    $value[11] = !empty($event[log_type])?"'$event[log_type]'":'NULL';
    $value[12] = !empty($event[service_group])?"'$event[service_group]'":'NULL';
    $value[13] = !empty($event[card_id])?"'$event[card_id]'":'NULL';

    $statement = "INSERT INTO `vrslog`.`log_send_popup`(
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
      '{$event[event]}',
      '{$httpCode}',
      '{$response}',
      '{$finish}'
    )";

    dbquery($statement);
  }
?>

