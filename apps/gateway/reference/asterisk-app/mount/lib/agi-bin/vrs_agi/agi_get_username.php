#!/usr/bin/php -q
<?php
  require_once 'lib/phpagi.php';
  require_once 'lib/func.db.php';

  // AGI ARGV Format
  //        extension
  // From : ${agent_num}

  $agi = new AGI();

  $strSQL = "SELECT `username` FROM `user_mapping` WHERE `extension` = {$argv[1]};";
  $res = dbquery($strSQL, DB_FETCH_ROW);

  $agi->verbose($res[username]);
  $agi->set_variable('__AGENT_USERNAME', $res[username]);
  return 0;
?>
