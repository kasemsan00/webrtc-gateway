#!/usr/bin/php -q
<?php
  require_once 'env_for_agi.php';
  require_once 'lib/phpagi.php';
  require_once 'lib/func.db.php';

  set_time_limit(5);
  date_default_timezone_set('Asia/Bangkok');

  $agi = new AGI();
  $keys = array(
    'action_at',
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
    $event[action_at] = GetTimestamp($agi);
    $action_at = $event[action_at];
    $event[finish] = SetIsFinish($agi, $event[event]);
    InsertEventToSql($event);
    $event[action_at] = ceil($event[action_at]);
    $payload = SetDataForXWwwFormUrlencoded($keys, $event);
    $event[path] = SetPathUrlSendEvent($event[event]);
    $response = CurlEvent($domain_event, $event[path], $payload, $agi);
    $resArr = array();
    $resArr = json_decode($response[response]);
    $response[body] = $resArr;
    $response[response] = '{"status":"'.$resArr->status.'","detail":"'.$resArr->detail.'"}';
    VerboseEvent($agi, $event, $response[response]);
    UpdateEventToSql($response, $action_at, $event[uniqueid], $event[event]);
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
    // $agi->verbose("Get Event From Asterisk");
    // $agi->verbose($event);
    // $agi->verbose();
    return $event;
  }
  
  function GetTimestamp($agi) {
    $action_at = microtime(true);
    // $agi->verbose("Get Timestamp");
    // $agi->verbose($action_at);
    // $agi->verbose();
    return $action_at;
  }
  
  function SetIsFinish($agi, $event) {
    if ($event == agentComplete || $event == queueCallerAbandon) {
      $finish = 'true';
    } else {
      $finish = 'false';
    }
    // $agi->verbose("Set Is Finish");
    // $agi->verbose($finish);
    // $agi->verbose();
    return $finish;
  }

  function InsertEventToSql($event) {
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
    $statement = "INSERT INTO `vrslog`.`log_send_popup`(
      `action_at`,
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
      '$event[action_at]',
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
      '0',
      NULL,
      '$event[finish]'
    )";

    dbquery($statement);
  }

  function SetDataForXWwwFormUrlencoded($keys, $value) {
    $value[uniqueid] = !empty($value[uniqueid])?$value[uniqueid]:' ';
    $value[source_number] = !empty($value[source_number])?$value[source_number]:' ';
    $value[source_name] = !empty($value[source_name])?$value[source_name]:' ';
    $value[destination_number] = !empty($value[destination_number])?$value[destination_number]:' ';
    $value[agent_number] = !empty($value[agent_number])?$value[agent_number]:' ';
    $value[agent_name] = !empty($value[agent_name])?$value[agent_name]:' ';
    $value[agent_username] = !empty($value[agent_username])?$value[agent_username]:' ';
    $value[source_type] = !empty($value[source_type])?$value[source_type]:' ';
    $value[call_type] = !empty($value[call_type])?$value[call_type]:' ';
    $value[log_type] = !empty($value[log_type])?$value[log_type]:' ';
    $value[service_group] = !empty($value[service_group])?$value[service_group]:' ';
    $value[card_id] = !empty($value[card_id])?$value[card_id]:' ';
    $payload = "";

    for ($i=0; $i < count($keys); $i++) {
      if ($payload != '') {
        $payload = $payload . '&';
      }
      $payload = $payload . $keys[$i] . '=' . $value[$keys[$i]];
    }

    return $payload;
  }

  function SetPathUrlSendEvent($event) {
    switch ($event) {
      case "ring":
        $path = "callRing";
        break;
      case "answer":
        $path = "callAnswer";
        break;
      case "agentComplete":
        $path = "callHangup";
        break;
      case "queueCallerAbandon":
        $path = "callAbandon";
        break;
      case "agentRingNoAnswer":
        $path = "callNoanswer";
        break;
      default:
        $path = "";
    }
    return $path;
  }

  function CurlEvent($domain, $route, $payload, $agi) {
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

  function VerboseEvent($agi, $event, $response) {
    $agi->verbose("----------------------------------------------------------------------------------------------");
    $agi->verbose("queue                " . $event[queue]);
    $agi->verbose("event                " . $event[event]);
    $agi->verbose("action_at            " . $event[action_at]);
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

  function UpdateEventToSql($response, $actionAt, $uniqueid, $event) {
    $statement = "UPDATE
      `vrslog`.`log_send_popup` 
    SET
      `http_code` = '$response[httpCode]',
      `response` = '$response[response]'
    WHERE
      `http_code` = '0'
    AND
      `action_at` = '$actionAt'
    AND
      `uniqueid` = '$uniqueid'
    AND
      `event` = '$event'";

    dbquery($statement);
  }
?>

