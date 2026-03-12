#!/usr/bin/php -q
<?php
  require_once 'lib/phpagi.php';
  require_once 'lib/func.db.php';

  // AGI ARGV Format
  //        id
  // From : ${NODEST}

  $agi = new AGI();

  $strSQL = "SELECT `data` FROM asterisk.`queues_details` WHERE `id` = {$argv[1]} AND `keyword` = 'timeout';";
  $res = dbquery($strSQL, DB_FETCH_ROW);
  
  $agi->verbose($res[data]);
  $agi->set_variable('__QUEUE_TIMEOUT', $res[data]);
  return 0;
?>
