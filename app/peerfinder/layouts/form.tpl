<div class="form-group"><label class="col-sm-2 control-label" >Peer Name</label><div class='col-sm-10'><input class="form-control" type=text name=peer_name  value="{$o.peer_name|default:''|escape}"/></div></div>
<div class="form-group"><label class="col-sm-2 control-label" >IRC Nick</label><div class='col-sm-10'><input class="form-control" type=text name=peer_nick  value="{$o.peer_nick|default:''|escape}"/></div></div>
<div class="form-group"><label class="col-sm-2 control-label" >Note</label><div class='col-sm-10'><input class="form-control" type=text name=peer_note  value="{$o.peer_note|default:''|escape}"/></div></div>
<div class="form-group"><label class="col-sm-2 control-label" >Country</label><div class='col-sm-2'><input class="form-control" type=text name=peer_country maxlength=3  value="{$o.peer_country|default:''|escape}"/></div></div>
<div class="form-group"><label class="col-sm-2 control-label" >VPN Types</label><div class='col-sm-10'><select class="form-control" size=12  multiple name="peer_type[]">
  <option {$types['openvpn']|default:''} value="openvpn">openvpn</option>
  <option {$types['gre/ipsec']|default:''} value="gre/ipsec">gre/ipsec</option>
  <option {$types['gre/plain']|default:''} value="gre/plain">gre/plain</option>
  <option {$types['fastd']|default:''} value="fastd">fastd</option>
  <option {$types['tinc']|default:''} value="tinc">tinc</option>
  <option {$types['zerotier']|default:''} value="zerotier">zerotier</option>
  <option {$types['wireguard']|default:''} value="wireguard">wireguard</option>
  <option {$types['pptp']|default:''} value="pptp">pptp</option>
  <option {$types['l2tp']|default:''} value="l2tp">l2tp</option>
  <option {$types['other']|default:''} value="other">other</option>
</select></div></div>
<div class="form-group"><label class="col-sm-2 control-label" >Address Family</label><div class='col-sm-10'>
  <label><input type="radio" name="peer_family"  value="1"   {$fam[0]|default:''} /> ipv4 </label>
  <label><input type="radio" name="peer_family"  value="2"   {$fam[1]|default:''} /> ipv6 </label>
  <label><input type="radio" name="peer_family"  value="3"   {$fam[2]|default:''} /> both </label>
</div></div>