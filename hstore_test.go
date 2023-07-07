package pgxtypefaster_test

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/evanj/pgxtypefaster"
	"github.com/jackc/pgx/v5/pgtype"
)

func fasterToOrig(h pgxtypefaster.Hstore) any {
	out := make(pgtype.Hstore, len(h))
	for k, v := range h {
		if v.Valid {
			// can't use &v.String: it takes the address of the loop variable
			s := v.String
			out[k] = &s
		} else {
			out[k] = nil
		}
	}
	return out
}

func fasterToCompat(h pgxtypefaster.Hstore) any {
	return pgxtypefaster.HstoreCompat(fasterToOrig(h).(pgtype.Hstore))
}

type hstoreTestCodecConfig struct {
	name                     string
	encodePlan               pgtype.EncodePlan
	scanPlan                 pgtype.ScanPlan
	fasterHstoreToConfigType func(h pgxtypefaster.Hstore) any
	newScanType              func() any
}

// allHstoreConfigs contains all the Hstore codecs to test.
var allHstoreConfigs = []hstoreTestCodecConfig{
	{
		"pgxtypefaster/text",
		pgxtypefaster.HstoreCodec{}.PlanEncode(nil, 0, pgtype.TextFormatCode, pgxtypefaster.Hstore{}),
		pgxtypefaster.HstoreCodec{}.PlanScan(nil, 0, pgtype.TextFormatCode, (*pgxtypefaster.Hstore)(nil)),
		func(h pgxtypefaster.Hstore) any { return h },
		func() any { return &pgxtypefaster.Hstore{} },
	},
	{
		"pgxtypefaster_compat/text",
		pgxtypefaster.HstoreCompatCodec{}.PlanEncode(nil, 0, pgtype.TextFormatCode, pgxtypefaster.HstoreCompat{}),
		pgxtypefaster.HstoreCompatCodec{}.PlanScan(nil, 0, pgtype.TextFormatCode, (*pgxtypefaster.HstoreCompat)(nil)),
		fasterToCompat,
		func() any { return &pgxtypefaster.HstoreCompat{} },
	},
	{
		"pgtype/text",
		pgtype.HstoreCodec{}.PlanEncode(nil, 0, pgtype.TextFormatCode, pgtype.Hstore{}),
		pgtype.HstoreCodec{}.PlanScan(nil, 0, pgtype.TextFormatCode, (*pgtype.Hstore)(nil)),
		fasterToOrig,
		func() any { return &pgtype.Hstore{} },
	},
	{
		"pgxtypefaster/binary",
		pgxtypefaster.HstoreCodec{}.PlanEncode(nil, 0, pgtype.BinaryFormatCode, pgxtypefaster.Hstore{}),
		pgxtypefaster.HstoreCodec{}.PlanScan(nil, 0, pgtype.BinaryFormatCode, (*pgxtypefaster.Hstore)(nil)),
		func(h pgxtypefaster.Hstore) any { return h },
		func() any { return &pgxtypefaster.Hstore{} },
	},
	{
		"pgxtypefaster_compat/binary",
		pgxtypefaster.HstoreCompatCodec{}.PlanEncode(nil, 0, pgtype.BinaryFormatCode, pgxtypefaster.HstoreCompat{}),
		pgxtypefaster.HstoreCompatCodec{}.PlanScan(nil, 0, pgtype.BinaryFormatCode, (*pgxtypefaster.HstoreCompat)(nil)),
		fasterToCompat,
		func() any { return &pgxtypefaster.HstoreCompat{} },
	},
	{
		"pgtype/binary",
		pgtype.HstoreCodec{}.PlanEncode(nil, 0, pgtype.BinaryFormatCode, pgtype.Hstore{}),
		pgtype.HstoreCodec{}.PlanScan(nil, 0, pgtype.BinaryFormatCode, (*pgtype.Hstore)(nil)),
		fasterToOrig,
		func() any { return &pgtype.Hstore{} },
	},
}

func init() {
	// validate allHstoreConfigs
	for _, config := range allHstoreConfigs {
		if config.encodePlan == nil {
			panic(fmt.Sprintf("%s encodePlan is nil (invalid arguments)", config.name))
		}
		if config.scanPlan == nil {
			panic(config.name)
		}
		out1 := config.fasterHstoreToConfigType(nil)
		out2 := config.newScanType()
		out2Type := reflect.TypeOf(out2)
		if out2Type.Kind() != reflect.Pointer {
			panic("newScanType() must return a pointer to an hstore type")
		}
		out2ElemType := out2Type.Elem()
		if reflect.TypeOf(out1) != out2ElemType {
			panic(fmt.Sprintf("%s: types of fasterHstoreToConfigType=%T and *newScanType()=%s must match",
				config.name, out1, out2ElemType.String()))
		}
	}
}

func BenchmarkHstoreEncode(b *testing.B) {
	input := pgxtypefaster.Hstore{
		"a x": pgxtypefaster.NewText("100"),
		"b":   pgxtypefaster.NewText("200"),
		"c":   pgxtypefaster.NewText("300"),
		"d":   pgxtypefaster.NewText("400"),
		"e":   pgxtypefaster.NewText("500"),
	}

	for _, hstoreConfig := range allHstoreConfigs {
		typeSpecificInput := hstoreConfig.fasterHstoreToConfigType(input)
		var buf []byte
		b.Run(hstoreConfig.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				var err error
				buf, err = hstoreConfig.encodePlan.Encode(typeSpecificInput, buf)
				if err != nil {
					b.Fatal(err)
				}
				buf = buf[:0]
			}
		})
	}
}

func BenchmarkHstoreScan(b *testing.B) {
	// empty, NULL, escapes, and based on some real data
	benchStrings := []string{
		"",
		`"a"=>"b"`,
		`"a"=>"100", "b"=>"200", "c"=>"300", "d"=>"400", "e"=>"500"`,
		`"a"=>"100", "b"=>NULL, "c"=>"300", "d"=>NULL, "e"=>"500"`,
		`"pmd"=>"piokifjzxdy:mhvvmotns:sf1-dttudcp-orx-fuwzw-j8o-tl-jcg-1fb5d6dp50ke3l24", "ausz"=>"aorc-iosdby_tbxsjihj-kss64-32r128y-i2", "mgjo"=>"hxcp-ciag", "hkbee"=>"bokihheb", "gpcvhc"=>"ne-ywik-1", "olzjegk"=>"rxbkzba", "iy_quthhf"=>"sryizraxx", "bwpdpplfz"=>"gbdh-jikmnp_jwugdvjs-drh64-32k128h-p2", "njy_veipyyl"=>"727006795293", "vsgvqlrnqadzvk"=>"1_7_43", "mfdncuqvxp_gqlkytj"=>"fuyin", "cnuiswkwavoupqebov"=>"x32n128w", "mol_lcabioescln_ulstxauvi"=>"qm1-adbcand-tzi-fpnbv-s8j-vi-gqs-1om5b6lx50zk3u24", "arlyhgdxux.fc/bezucmz/mmfed"=>"vihsk", "jtkf.czddftrhr.ici/qbq_ftaz"=>"sse64", "notxkfqmpq.whxmykhtc.bcu/zmxz"=>"zauaklqp-uwo64-32q128a-g2", "ww_affdwqa_o8o_ilskcucq_urzltnf"=>"i6-9-0", "f8d.eq/bbqxwru-vsznvxerae/wsszbjw"=>"dgd", "ygpghkljze.dkrlrrieo.iur/xfqdqreft"=>"pfby-bhqlmm", "pmho-dqxuezyuu.ppslmznja.eam/ikehtxg"=>"wbku", "ckqeavtcqk.jiqdipgji.hjl/luzgqb-agm-wb"=>"ikpq", "akcn-yobdpxkyl.gktsjdo-xqwmivixku.p8y.vq/axqdw"=>"", "r8u.at/fbqrrss-ihxjmygoyc/ztqe-pqqqewnz/nepdj/njjv"=>"txtlffpp:ebwdksxkej", "q8x.wu/wenlhkz-govetdoibn/rcwg-ticalfjq/mgipy/awmjl"=>"dyzvbzvi", "p8l.wx/vadrnki-yfqhzlwcnt/hvun-geqhjsik/eqediipfr/vlc"=>"31900z", "t8z.be/qbtsmci-jqnqphssdg/ejma-slvywzry/txpnybwvn/kxdl"=>"210", "o8b.nb/bijgpwm-axvvqgujax/fjli-mxqwulfe/revyfoyty/oojpsd"=>"123421925786", "p8q.sk/ccpgzee-ufjempgvty/afwh-qvwzjvog/hsyhr/bklplujbfydtfw"=>"1_7_43", "k8y.jp/hqoymrw-flwqwvbntf/dlli-uggxkdqv/mtutu/qotjmacjitwtvcnblr"=>"m32x128f", "r8z.hj/eczodcw-lxzmeeqqii/fjba-psyoidht/gfjjcdbqs/apkqxiznu-muzubvl"=>"106068512341", "u8v.nf/ocnahkw-prhuwrrbjg/gxms-isohcouc/txfle/zfzw.neyygeeur.ejv/rnd_vdyo"=>"ibx64", "i8c.zz/dtiulqn-mmbskzjcib/fxuj-ejxbrnqi/optyp/wbbrancspv.pnkizgxcj.dbm/bldn"=>"znppnwzg-oxp64-32r128h-d2", "d8t.dg/jqtodoh-sokunyljow/svdf-ghplxxcx/wqkwl/dolljeqv.jcn.dxp.jmh.uyf/lyfv"=>"kc-lmpu-1i", "t8i.dy/imltbpr-atmthzarmk/fbbw-uaovyvdj/mmuwq/kseu-snmt.xtlgkstzph.mg/ehjdpgc"=>"", "o8c.yc/wximcpf-wmffadvnxx/tdim-szbqedqp/ztrui/puhx-kcwp.zziulqvvmb.ik/khfaxajj"=>"", "j8i.zc/sajavzi-kemnitliml/nloy-riqothpw/yxmnp/ttrnynffzy.lswpezbdq.wor/xkvqeexio"=>"ltmp-zajsxt", "a8f.xd/tfrrawy-ymihugugaa/ouzi-xdyecmqx/cwvgjvcrh/trgbxgbumo.uh/xmnqbds-nqxxeuqpq"=>"3123748065", "x8n.vx/juiqxkj-swvwogmncw/hvad-pojmevog/ytxit/auvo-duchssbth.uickilmnz.lja/hbeiakj"=>"hwhd", "z8j.bn/iplhrhv-wjdcwdclos/qndu-qvotchss/spvfx/brqotjnytw.aaemsoxor.ign/uwebjm-vzl-kb"=>"zwdg", "t8j.vx/iekvskm-xhikarvbty/czlm-xtipxwok/eeeow/uvtpuzmlqg.jgtpgiujc.wrs/mcofa-qxjjwak"=>"sovxb", "t8g.ab/wuncjdz-vsozsekgxz/aaea-hmgdjylm/qimwsoecgud-grgoowb/zveahbidvwcaebhlzigytiermehxy"=>"0.95", "n8k.ei/ohovibm-obkaatwlyw/bcow-gndyzpyt/aehyf/dpgifsorjx.ehsqntrka.jrr/meakdzy-ckxgnfavwm"=>"nlgw", "u8e.yi/qavbjew-qnmtzbeyce/rmwa-hcqlvadn/bhpml/taoj-wjnh.qqvkjmccfn.ja/nudbtwme-buc64-32j128i-k2"=>""`,
		`"mbgs"=>"eqjillclhkxz", "bxci"=>"etksm.rudiu", "jijqqm"=>"kj-ryxhwqtco-2", "yivvcxy"=>"fwbujcu", "ybk_ztlajai"=>"601427279990"`,
		`"wte"=>"nrhw", "lqjm"=>"ifsbygchn", "wbmf"=>"amjsoykkwq\\ghvwbsmz-qeiv-iekd-ukcwbipzy"`,
		`"otx"=>"fcreomqbwtk:gqhxzhxuh:wrqo-rf1-avhdpfy-nqi-dldof-i8p-mw-jll-l5r9741753c3", "vbjy"=>"akzfspigip_muzyxzwuso-zvoifh-uw", "fmkb"=>"pkoe-lezf", "wfbq"=>"qoviagajeg", "zvxbiv"=>"db-bcngmoq-1", "olictqnpx"=>"taqcnrcwcj_ticfxydekq-fafbkg-ot", "wkt_jtzzqpt"=>"727006795293", "bsdncvmbvj_xivgkws"=>"zczag", "muzq.oyrphhtne.fqm/itc"=>"ihilzgx", "pfsd.xphmjdohu.hrm/yeimpfm"=>"lrrqxrwyud-uvcljo", "qukdxappwo.or/xgcsmdo/dodoj"=>"onflq", "ktqrsqtllo.xxxpkizlg.tnf/unrt"=>"jrveutvddu-loihei-ww", "tr_qmarsis_s8v_skzbuuvy_cnyuxyk"=>"g6-16-0", "z8q.yc/xistcyy-tftbikuuhg/zvhemmi"=>"knv", "zrgwpjnvzq.twkcxxuyk.qwc/nirbacaom"=>"okfdlcpbdg", "suvk-wwwjqdytq.wdjmzxl-nduettmnmf.e8e.ec/qhkan"=>"", "u8m.xa/uvbhlmw-rqrcyyaiju/otsg-bqjfitoq/zqfuq/fifo"=>"brarmrogdb", "b8o.ci/znwkyby-nzuxiguqus/nwou-cxxnqxrr/rtdsp/yawv"=>"juedpptnbt-khocdt-vg:vfxpdswxnc", "u8h.vl/kgmvysr-xhykrjcssj/jfjv-gzalgika/yhrjfytwz/kbm"=>"3900f", "y8b.cm/ttijscl-rznjossaqw/kvto-gvnavnep/bwdqyuzgo/ozoi"=>"40", "p8j.pd/bnucngv-vnqufgvfqw/qshw-obnkmlfx/obczheyis/zzbsos"=>"7009zf", "p8y.fc/ejbndrq-aariupaovi/mrah-hmrhjcsv/lvrmfwwiz/uskogxfuw-zamygae"=>"18747532246", "y8m.oh/xzuhilr-wqmqqzcznb/pcox-idpxmhfj/yzsoj/qebkjaeymc.abqznnelq.gyd/osvb"=>"hsgxlccalq-eeybug-mx", "p8f.ay/tyntrss-nljxedfihd/grvy-znfykhlf/fjsqd/ffxaixyv.jie.bkg.zpd.kim/mgtc"=>"or-vrkdcxm-1i", "i8m.ms/jtykfbi-jdrqsqjdwt/ibaq-zmeuyznf/uczny/ufmj-zklt.omodkgubqw.ip/xztdevd"=>"", "k8m.ui/ymxurqo-kuhofnewjj/twex-iuwljutj/warlx/zptkdgqdpr.uhvqtrclx.ohj/bdkgsozkk"=>"zlgisdikac", "g8b.wk/vecudfr-pljllpgzxi/lbwd-zsracrgq/fucssaowj/syizbmlfqt.si/swpbend-gxrhddxad"=>"156213905", "z8y.ah/azeasta-gffxfwklrn/hukw-hphwntwy/lfswv/tmaeaxekya.vgkxjhtvg.mht/bzolt-koioxpf"=>"wzkra", "f8l.sy/ouekhco-rlhsclfzwx/erfz-uuejogrs/bgvia/zpohrhmrmu.sbdxzlaxo.wii/jbnwfvz-shekbewool"=>"aiey", "j8w.pz/fjtkxhn-zxxizfldde/wsik-uiodldga/ljdtl/gswz-cjmt.ffkelhxcsd.lw/ftcqgdnnho-ibbfql-ww"=>""`,
		`"uvd"=>"oneotg", "wsm"=>"djjgmwqyple:jtxtfvtjv:du1-nfxzmra-idl-ikxbx-t8n-id-nbo-6d08opx70381", "orq"=>"bkdvjw-xydgbd1", "gblm"=>"jtkcfd-unxbag1_xagyfw-nvachf1", "mfer"=>"jclz-yaim", "jvgvas"=>"jf-vhxh-1", "wwardeuqu"=>"ufimeb-bscfdy1_bfuagy-dhdqra1", "szs_rfgpqmc"=>"727006795293", "ckfxcgrnqc_rloxzxu"=>"qffbw", "yaigdvscju.ba/krpgzji/wvxyg"=>"srgtu", "gtxfjsigdv.pxujnffnp.aza/ycco"=>"ntranp-ahgeem1", "xj_lhdpvsl_i8i_qzrtlpjr_nroujqh"=>"q6-1-8", "czxy-sfym.enlohvvjmp.wb/huvcuhy"=>"", "x8a.of/sqpdqiq-vijrlgkkyl/oncckls"=>"mij", "oomgvfopmc.trnzktrtz.gza/rpeqqyqmm"=>"rgwnma-bwcbxe1", "gaud-giar.xuablvwkbo.wy/wvhmsk-uaycqn1"=>"", "oarbmcqzzw.qkfbtmltz.plh/aqssj-tlrhsof"=>"wxfd", "zepirccplb.qanvqnxlo.eld/emulnov-vgddsefeqv"=>"jnvh", "acby-kywxjuczc.suosfcy-drsgroeqvy.o8m.og/vyuxt"=>"", "q8j.by/lrwxbjt-yzrenlniog/gbmw-mnokcndu/etbcy/ibwr"=>"qpttug-jnxhwe1:grmslxhyky", "i8y.uy/awavkxk-nztmqujxys/pocu-sqjdqvzd/tfdjeflpn/xsj"=>"7900c", "z8g.ia/yzfdvta-ffkciorpfl/kmjc-fgcdomlv/snvhhbjil/nhvn"=>"45", "s8l.ky/dtvxoqu-lzfdnykmdh/wtdg-aktximmy/hofzkpzel/wtghso"=>"14837zg", "v8e.rq/uosznaz-drypoapgpe/vxss-mbxmvkjj/oglvxhxcz/whutvtjmr-tewtidr"=>"18747532246", "m8p.sz/hrgniti-aufhjdsdcc/whcp-cfuwjsnl/exugj/evphviokhl.ashpndixr.jvx/vgtt"=>"zdsacy-ppfuxf1", "w8t.fm/kljwjgc-fijbwsrvxa/dbzl-fhxvlrwk/yidyk/orrt-kgpr.wuzmpnxvtb.lc/dmbqfvt"=>"", "m8j.sv/takylmm-ywnolaflnl/ueih-fdcpfcpv/dslbc/dsspusnhtu.vgkihqtpb.fto/qmyksglfx"=>"wpwuih-deuiej1", "m8x.wi/jwobkio-mwupghbqbi/krqn-hqyfgwuw/mcbyi/yzkt-wtdy.pjxevrogab.tj/qlttbz-ppyzkd1"=>"", "c8j.tr/tzcbhid-lggaiypnny/wyms-zcjgxmwp/eaohd/bcwkheknsr.fqvtgecsf.qbf/uaqzj-jburpix"=>"ckkk", "w8h.wk/msbqvqy-nsmvbojwns/edpo-nsivbrmx/qifaf/sopuabsuvq.foyniwomd.zvj/lhvfwvv-zuufhhspso"=>"fghx"`,
		`"xxtlvd"=>"ba-zrzy-1", "hlebkcl"=>"entrcad", "ytn_toivqso"=>"601427279990", "czdllqyvkcfemhubpwvxakepubup"=>"jzhpff-vn2-sgiupfiii-qmuuz-ndex-vin-kmfm", "mefjcnjmcspgviisjalxmwdbksmge"=>"2022-11-20"`,
		`"ukq"=>"uhkbdj", "bmj"=>"mcoknsnhqcb:vmexvsccu:yt1-nscwdfr-zcp-ajfhr-z8i-ta-jhv-58yl03459t86", "cuq"=>"sqphbh-xkxbcgwdx", "dnac"=>"khzjpq-hljdvlbsw_azdisd-nshizhinc", "flgj"=>"zeem-pggu", "ksnn"=>"vpittgnl-xeojllby-toq", "wwxepg"=>"ki-cwee-1", "vigdnntxw"=>"sydsls-zidlsgugi_wviqvl-umwzyztab", "osz_utmlghi"=>"727006795293", "wacdaefqhc_buqmsci"=>"djtcv", "ljdbotgrsi.xn/gvtjfeg/iiyek"=>"lnfgg", "sohcclfodf.wkwiitult.ppm/hhsf"=>"ecpftm-ecmsibfjy", "dz_cgfnddq_o8j_cowdxlfz_rmjunpm"=>"v5-13-1", "niwk-fozq.tbamcxrhez.kl/zuxnisw"=>"", "h8k.xu/nbsezqz-fopcyqlnwt/lfcmgag"=>"dmm", "zebgpskksd.daigyeicb.dlj/dwmcpkohh"=>"hegecl-bnqmkunkl", "irjreiuove.qpmjixctw.mzv/xizjv-bpecdmy"=>"rkfl", "fupz-eiim.hwaqzvpzgv.yg/zhrqmr-qcydocyak"=>"", "djuscbflju.fmhnephvc.cmo/wzcisia-kqmrrhnkiv"=>"vchu", "hauo-olkeyvbrz.qzpaocu-wdbyfrzjkx.c8a.rn/bwhfe"=>"", "l8d.fj/jzojrmv-mbnxftbdzg/qvgo-oayrldze/tqmoa/oizo"=>"buwgyd-bjlrzrlci:ywosrfsnts", "q8l.sj/vifqvao-ynvfejvleb/ourc-jzridgtt/fgxnueuvm/wsg"=>"7900p", "d8b.mi/steijrv-bgajdbugff/kxkj-jhvctoxw/seyrafhni/xxrc"=>"45", "x8k.bn/dnnkttb-ywqrwwxirk/ngvt-eqyaeqsd/qesxmjfos/nlolbe"=>"14837xp", "v8o.az/vtbyyyo-rjuadsmwyb/gszv-ytnisfau/kfunvihsr/famkeacyo-skpueao"=>"18747532246", "f8w.ip/sjzrxbw-idgsgucprq/ster-zxiilwcf/luwzw/tavccuqfph.mcubdrtcr.ibw/dxnj"=>"ntyjnf-zwlyjqbfq", "y8f.mh/qykpkfr-fsnlckrhpe/hvyu-vstwrxkq/dmesn/kuor-acub.fqwqxcpiet.jf/zaxtdyb"=>"", "c8m.et/ekavnnp-gvpmldvoou/jzva-zzzpiecc/dvckb/qqxrfpoaiy.ssfqerwmb.cnz/odsfndorh"=>"liilkb-aekfuqzss", "e8n.gp/sybrxvz-mghjbpphpc/wcuo-naanbtcj/agtov/dztlgdacuz.fpbhhiybg.ncm/otgfu-hnezrwu"=>"ccez", "t8h.cy/bqsdiil-lxmioonwjt/drsw-qevzljvt/rvzjl/btbz-npvi.ypyxowgmfp.gf/jcfbyh-khpgbaayw"=>"", "y8b.df/anmudfn-gahfengbqw/fhdi-ozqtddmu/lvviu/kndwvowlby.jxkizwkac.hbq/fjkqyna-jijxahivma"=>"wxqg"`,
	}

	// convert benchStrings into text and binary bytes
	textBytes := make([][]byte, len(benchStrings))
	binaryBytes := make([][]byte, len(benchStrings))
	codec := pgtype.HstoreCodec{}.PlanEncode(nil, 0, pgtype.BinaryFormatCode, pgtype.Hstore(nil))
	for i, s := range benchStrings {
		textBytes[i] = []byte(s)

		var tempH pgtype.Hstore
		err := tempH.Scan(s)
		if err != nil {
			b.Fatal(err)
		}
		binaryBytes[i], err = codec.Encode(tempH, nil)
		if err != nil {
			b.Fatal(err)
		}
	}

	// benchmark the database/sql.Scan API
	var fasterH pgxtypefaster.Hstore
	b.Run("pgxtypefaster/databasesql.Scan", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			for _, str := range benchStrings {
				err := fasterH.Scan(str)
				if err != nil {
					b.Fatal(err)
				}
			}
		}
	})
	var fasterCompatH pgxtypefaster.HstoreCompat
	b.Run("fastercompat/databasesql.Scan", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			for _, str := range benchStrings {
				err := fasterCompatH.Scan(str)
				if err != nil {
					b.Fatal(err)
				}
			}
		}
	})
	var h pgtype.Hstore
	b.Run("pgtype/databasesql.Scan", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			for _, str := range benchStrings {
				err := h.Scan(str)
				if err != nil {
					b.Fatal(err)
				}
			}
		}
	})

	// benchmark the []byte scan API used by pgconn
	// for _, scanConfig := range scanConfigs {
	for _, hstoreConfig := range allHstoreConfigs {
		// hack to select between the text format input and binary format input
		inputBytes := textBytes
		if strings.HasSuffix(hstoreConfig.name, "/binary") {
			inputBytes = binaryBytes
		}

		scanArg := hstoreConfig.newScanType()
		b.Run(hstoreConfig.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				for _, input := range inputBytes {
					err := hstoreConfig.scanPlan.Scan(input, scanArg)
					if err != nil {
						b.Fatalf("input=%#v err=%s", string(input), err)
					}
				}
			}
		})
	}
}
