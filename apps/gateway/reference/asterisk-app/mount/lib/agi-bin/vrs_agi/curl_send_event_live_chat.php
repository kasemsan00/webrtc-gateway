#!/usr/bin/php -q
<?php
  require_once 'env_for_agi.php';
  require_once 'lib/phpagi.php';
  require_once 'lib/func.db.php';


  $agi = new AGI();
  $keys = array(
    'create_at',
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
  $event[create_at] = date('Y-m-d H:i:s');
    
  if ($queue_for_live_chat[$event[queue]] == 1) {
    $setEvent = SetEvent($event);
    $payload = SetDataForXWwwFormUrlencoded($keys, $setEvent);
    // $response = CurlEvent($domain_event_live_chat, $setEvent[route], $payload);
    $response = pecl_http($domain_event_live_chat, $setEvent[route], $setEvent);
    
    VerboseEvent($agi, $event, $response[response]);
    WriteErrorLog($keys, $setEvent, $response[httpCode], $event[event], $response[response]);
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
    switch ($event[event]) {
      case 'ring':
        $value[route] = "ring";
        break;

      case 'answer':
        $value[route] = "answer";
        break;

      case 'agentComplete':
        $value[route] = "hangup";
        break;

      case 'queueCallerAbandon':
        $value[route] = "queue_caller_abandon";
        break;

      case 'agentRingNoAnswer':
        $value[route] = "no_answer";
        break;
    }

    $value[create_at] = !empty($event[create_at])?$event[create_at]:' ';
    $value[uniqueid] = !empty($event[uniqueid])?$event[uniqueid]:' ';
    $value[source_number] = !empty($event[source_number])?$event[source_number]:' ';
    $value[source_name] = !empty($event[source_name])?$event[source_name]:' ';
    $value[destination_number] = !empty($event[destination_number])?$event[destination_number]:' ';
    $value[agent_number] = !empty($event[agent_number])?$event[agent_number]:' ';
    $value[agent_name] = !empty($event[agent_name])?$event[agent_name]:' ';
    $value[agent_username] = !empty($event[agent_username])?$event[agent_username]:' ';
    $value[source_type] = !empty($event[source_type])?$event[source_type]:' ';
    $value[call_type] = !empty($event[call_type])?$event[call_type]:' ';
    $value[log_type] = !empty($event[log_type])?$event[log_type]:' ';
    $value[service_group] = !empty($event[service_group])?$event[service_group]:' ';
    $value[card_id] = !empty($event[card_id])?$event[card_id]:' ';
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
      if ($payload != '') {
        $payload = $payload . '&';
      }

      $payload = $payload . $keys[$i] . '=' . $value[$keys[$i]];
    }

    return $payload;
  }

  function VerboseEvent($agi, $event, $response) {
    $agi->verbose("----------------------------------------------------------------------------------------------");
    $agi->verbose("create_at            " . $event[create_at]);
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
    $payload = $payload . $value[$keys[0]] . ' ';
    for ($i=1; $i < count($keys); $i++) {
      if ($i > 1)
        $payload = $payload . ', '; 
      
      $payload = $payload . $keys[$i] . ' = ' . $value[$keys[$i]];
    }
    $payload = $payload . ', ' . "http code" . ' = ' . $httpCode . ', ' . "event" . ' = ' . $event . ', ' . $response;
    error_log($payload."\n", 3, "/var/log/asterisk/log_send_event_live_chat.log");
  }

  function InsertEventToSql($event, $httpCode, $response) {
    if ($event[event] == agentComplete || $event[event] == queueCallerAbandon)
      $finish = 'true';
    else
      $finish = 'false';

    $event[create_at] = !empty($event[create_at])?"'$event[create_at]'":'NULL';
    $event[uniqueid] = !empty($event[uniqueid])?"'$event[uniqueid]'":'NULL';
    $event[source_number] = !empty($event[source_number])?"'$event[source_number]'":'NULL';
    $event[source_name] = !empty($event[source_name])?"'$event[source_name]'":'NULL';
    $event[destination_number] = !empty($event[destination_number])?"'$event[destination_number]'":'NULL';
    $event[agent_number] = !empty($event[agent_number])?"'$event[agent_number]'":'NULL';
    $event[agent_name] = !empty($event[agent_name])?"'$event[agent_name]'":'NULL';
    $event[agent_username] = !empty($event[agent_username])?"'$event[agent_username]'":'NULL';
    $event[source_type] = !empty($event[source_type])?"'$event[source_type]'":'NULL';
    $event[call_type] = !empty($event[call_type])?"'$event[call_type]'":'NULL';
    $event[log_type] = !empty($event[log_type])?"'$event[log_type]'":'NULL';
    $event[service_group] = !empty($event[service_group])?"'$event[service_group]'":'NULL';
    $event[card_id] = !empty($event[card_id])?"'$event[card_id]'":'NULL';

    $statement = "INSERT INTO `vrslog`.`log_send_popup_live_chat`(
      `create_at`,
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
      $event[create_at],
      $event[uniqueid],
      $event[source_number],
      $event[source_name],
      $event[destination_number],
      $event[agent_number],
      $event[agent_name],
      $event[agent_username],
      $event[source_type],
      $event[call_type],
      $event[log_type],
      $event[service_group],
      $event[card_id],
      '$event[event]',
      '$httpCode',
      '$response',
      '$finish'
    )";
    
    dbquery($statement);
  }

  function pecl_http($domain, $route, $value) {
    $postdata = http_build_query(
        array(
          'create_at' =>  $value[create_at],
          'uniqueid' => $value[uniqueid],
          'source_number' => $value[source_number],
          'source_name' => $value[source_name],
          'destination_number' => $value[destination_number],
          'agent_number' => $value[agent_number],
          'agent_name' => $value[agent_name],
          'agent_username' => $value[agent_username],
          'source_type' => $value[source_type],
          'call_type' => $value[call_type],
          'log_type' => $value[log_type],
          'service_group' => $value[service_group],
          'card_id' => $value[card_id]
        )
    );
    $opts = array('http' =>
        array(
            'method'  => 'POST',
            'header'  => 'Content-type: application/x-www-form-urlencoded',
            'content' => $postdata,
            'timeout' => 2
        )
    );
    $context  = stream_context_create($opts);
    $result = file_get_contents($domain . $route, false, $context);
    
    $response[domain] = $domain;
    $response[route] = $route;
    preg_match('#HTTP/[0-9\.]+\s+([0-9]+)#', $http_response_header[0], $out);
    $response[httpCode] = intval($out[1]);
    $response[response] = $result;
    return $response;
  }
?>
