<?php
  require_once 'lib/func.db.php';

  $strSQL = "SELECT * FROM `vrslog`.`log_service` WHERE `ls_uniqueid` >= '1716778599.1755' AND `ls_uniqueid` <= '1716864896.14594'";
  // $strSQL = "SELECT * FROM `vrslog`.`log_service` WHERE `ls_uniqueid` in ('1716778708.1814')";

  $results = dbquery($strSQL, DB_FETCH_ALL);

  foreach ($results as $key => $result) {
    $queue_begin = 0;
    $waiting_duration = 0;
    $service_duration = 0;
    $total_duration = 0;

    $strSQL = "SELECT * FROM `asteriskcdrdb`.`queue_log` WHERE `callid` = $result[ls_uniqueid]";
    $res = dbquery($strSQL, DB_FETCH_ALL);
    if (!empty($res)) {
      foreach ($res as $key => $r) {
        if ($r["event"] == "ENTERQUEUE") {  
          $queue_begin_milli = explode(".",$r["time"]);
          $queue_begin = $queue_begin_milli[0];
        }

        if ($r["event"] == "COMPLETECALLER" || $r["event"] == "COMPLETEAGENT") {
          $waiting_duration = $r["data1"];
          $service_duration = $r["data2"];
          $total_duration = $waiting_duration + $service_duration;
        }

        if ($r["event"] == "ABANDON") {
          $waiting_duration = $r["data3"];
          $total_duration = $r["data3"];
        }
      }

      $strSQL = "UPDATE `log_service` SET `queue_begin` = '$queue_begin', `waiting_duration` = '$waiting_duration', `service_duration` = '$service_duration', `total_duration` = '$total_duration' WHERE `ls_uniqueid` = '{$result["ls_uniqueid"]}'";

      dbquery($strSQL);
      echo $strSQL . "\n";
    }
  }


  return 0;
?>

