#!/usr/bin/php -q
<?php
  require_once 'lib/phpagi.php';
  require_once 'lib/func.db.php';

  // AGI ARGV Format
  //        agent
  // From : ${DB(AMPUSER/${QAGENT}/cidname)}

  $agi = new AGI();
  $USER_UNIQUEID = $agi->get_variable("USER_UNIQUEID")[data];

  $strSQL = "SELECT * FROM asteriskcdrdb.`queue_log` WHERE `callid` = $USER_UNIQUEID AND `agent` = '$argv[1]' AND `event` = 'RINGNOANSWER';";
  $res = dbquery($strSQL, DB_FETCH_ROW);
  
  $agi->verbose($res[data1]);
  $agi->set_variable('__RINGTIME', $res[data1]);
  return 0;
?>

