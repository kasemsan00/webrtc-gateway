#!/usr/bin/php -q
<?php
  require_once 'lib/phpagi.php';
  require_once 'lib/func.db.php';

  $agi = new AGI();
  $USER_CALL_ID = $agi->get_variable("USER_CALL_ID")['data'];
  $queue = $agi->get_variable("EMS_QUEUE")['data'];

  $strSQL = "SELECT * FROM `asteriskcdrdb`.`queue_log` WHERE `callid` = '$USER_CALL_ID' AND `queuename` = '$queue' AND `event` = 'ABANDON'";
  $res = dbquery($strSQL, DB_FETCH_ROW);
  
  $agi->set_variable('status_abandon_from_queue_log', $res[event]);
  return 0;
?>

