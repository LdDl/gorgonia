package gorgonia

import (
	"fmt"
	//"github.com/gorgonia"
	"hash"
	"image"
	"math"

	"github.com/chewxy/hm"
	"github.com/pkg/errors"
	"gorgonia.org/tensor"
)

type yoloOp struct {
	anchors     []float64
	mask        []int
	ignoreTresh float64
	inpDim      int
	numClasses  int
	train       bool
	target      *tensor.Dense
	gridSize    int
}

func newYoloOp(anchors []float64, mask []int, imheight, numclasses int, ignoreTresh float64, train bool) *yoloOp {
	yoloOp := &yoloOp{
		anchors:     anchors,
		inpDim:      imheight,
		numClasses:  numclasses,
		ignoreTresh: ignoreTresh,
		mask:        mask,
		train:       train,
	}
	return yoloOp
}

//YoloDetector yolov3 output layer
func YoloDetector(x *Node, anchors []float64, mask []int, imheight, numclasses int, ignoreTresh float64, target ...*tensor.Dense) (*Node, error) {
	if len(target) > 0 {
		op := newYoloOp(anchors, mask, imheight, numclasses, ignoreTresh, true)
		op.target = target[0]
		op.gridSize = x.Shape()[2]
		retVal, err := ApplyOp(op, x)
		return retVal, err
	}
	op := newYoloOp(anchors, mask, imheight, numclasses, ignoreTresh, false)
	retVal, err := ApplyOp(op, x)
	return retVal, err
}

func (op *yoloOp) Arity() int {
	return 1
}

func (op *yoloOp) ReturnsPtr() bool { return false }

func (op *yoloOp) CallsExtern() bool { return false }

func (op *yoloOp) WriteHash(h hash.Hash) {
	fmt.Fprintf(h, "Yolo{}(anchors: (%v))", op.anchors)
}
func (op *yoloOp) Hashcode() uint32 { return simpleHash(op) }

func (op *yoloOp) String() string {
	return fmt.Sprintf("Yolo{}(anchors: (%v))", op.anchors)
}
func (op *yoloOp) InferShape(inputs ...DimSizer) (tensor.Shape, error) {
	s := inputs[0].(tensor.Shape).Clone()
	if op.train {
		return []int{s[0], s[2] * s[3] * len(op.mask), (s[1]) / len(op.mask)}, nil
	}
	return []int{s[0], s[2] * s[3] * len(op.mask), s[1] / len(op.mask)}, nil
}

func (op *yoloOp) Type() hm.Type {

	a := hm.TypeVariable('a')
	t := newTensorType(4, a)
	o := newTensorType(3, a)
	return hm.NewFnType(t, o)

}
func (op *yoloOp) OverwritesInput() int { return -1 }

func (op *yoloOp) checkInput(inputs ...Value) (tensor.Tensor, error) {

	//Delete?
	if err := checkArity(op, len(inputs)); err != nil {
		return nil, err
	}

	var in tensor.Tensor
	var ok bool
	if in, ok = inputs[0].(tensor.Tensor); !ok {
		return nil, errors.Errorf("Expected input to be a tensor")
	}

	if in.Shape().Dims() != 4 {
		return nil, errors.Errorf("Expected input to have 4 dimensions")
	}
	return in, nil
}

func sigmSlice(v tensor.View, old error) {
	fmt.Println(v.Shape())
	if old != nil {
		panic(old)
	}
	switch v.Dtype() {
	case Float32:
		if _, err := v.Apply(_sigmoidf32, tensor.UseUnsafe()); err != nil {
			panic(err)
		}
	case Float64:
		if _, err := v.Apply(_sigmoidf64, tensor.UseUnsafe()); err != nil {
			panic(err)
		}
	default:
		panic("Unsupportable type for Yolo")
	}
}

func expSlice(v tensor.View, old error) {
	if old != nil {
		panic(old)
	}
	switch v.Dtype() {
	case Float32:
		if _, err := v.Apply(func(x float32) float32 {
			return float32(math.Exp(float64(x)))
		}, tensor.UseUnsafe()); err != nil {
			panic(err)
		}
	case Float64:
		if _, err := v.Apply(math.Exp, tensor.UseUnsafe()); err != nil {
			panic(err)
		}
	default:
		panic("Unsupportable type for Yolo")
	}
}

func convertToFloat32(in []float64) []float32 {
	out := make([]float32, len(in))
	for i := range in {
		out[i] = float32(in[i])
	}
	return out
}

func (op *yoloOp) Do(inputs ...Value) (retVal Value, err error) {
	if !op.train {
		in, _ := op.checkInput(inputs...)
		batch := in.Shape()[0]
		stride := int(op.inpDim / in.Shape()[2])
		grid := in.Shape()[2]
		bboxAttrs := 5 + op.numClasses
		numAnchors := len(op.mask)
		currentAnchors := []float64{}
		for _, i := range op.mask {
			if i >= (len(op.anchors) / 2) {
				return nil, errors.New("Incorrect mask for anchors on yolo layer with name" + fmt.Sprint(op.mask))
			}
			currentAnchors = append(currentAnchors, op.anchors[i*2], op.anchors[i*2+1])
		}
		fmt.Println(currentAnchors, op.anchors, in.Shape()[2], int(op.inpDim/stride))
		return op.yoloDoer(in, batch, stride, grid, bboxAttrs, numAnchors, currentAnchors)
	}
	in, _ := op.checkInput(inputs...)
	target := op.target
	//inv, _ := in.Slice(nil, S(0, in.Shape()[1]-1), nil, nil)
	numTargets, _ := target.At(0, 0, 0, 0)
	batch := in.Shape()[0]
	stride := int(op.inpDim / in.Shape()[2])
	grid := in.Shape()[2]
	bboxAttrs := 5 + op.numClasses
	numAnchors := len(op.mask)
	currentAnchors := []float64{}
	for _, i := range op.mask {
		if i >= (len(op.anchors) / 2) {
			return nil, errors.New("Incorrect mask for anchors on yolo layer with name" + fmt.Sprint(op.mask))
		}
		currentAnchors = append(currentAnchors, op.anchors[i*2], op.anchors[i*2+1])
	}
	var targets []float32
	switch in.Dtype() {
	case Float32:
		lt := int(numTargets.(float32))
		targets = make([]float32, lt, lt)
		for i := 1; i <= lt; i++ {
			buf, _ := target.At(0, target.Shape()[1]-1, 0+i/grid, 0+i%grid)
			targets[i-1] = buf.(float32)
		}
		break
	case Float64:
		lt := int(numTargets.(float64))
		targets = make([]float32, lt, lt)
		for i := 1; i <= lt; i++ {
			buf, _ := target.At(0, target.Shape()[1]-1, 0+i/grid, 0+i%grid)
			targets[i-1] = float32(buf.(float64))
		}
		break
	default:
		panic("Unsupportable type for Yolo")
	}
	err = in.Reshape(batch, bboxAttrs*numAnchors, grid*grid)
	if err != nil {
		return nil, errors.Wrap(err, "Can't make reshape grid^2 for YOLO v3")
	}

	err = in.T(0, 2, 1)
	if err != nil {
		return nil, errors.Wrap(err, "Can't safely transponse input for YOLO v3")
	}
	err = in.Transpose()
	if err != nil {
		return nil, errors.Wrap(err, "Can't transponse input for YOLO v3")
	}
	err = in.Reshape(batch, grid*grid*numAnchors, bboxAttrs)
	if err != nil {
		return nil, errors.Wrap(err, "Can't reshape bbox for YOLO v3")
	}
	rin := in.Clone().(tensor.Tensor)
	outyolo, _ := op.yoloDoer(in, batch, stride, grid, bboxAttrs, numAnchors, currentAnchors)
	fmt.Println(rin)
	yboxes32 := make([]float32, 0)
	input32 := make([]float32, 0)
	switch outyolo.Dtype() {
	case Float32:
		rin.Reshape(in.Shape()[0] * in.Shape()[1] * in.Shape()[2])
		outyolo.Reshape(outyolo.Shape()[0] * outyolo.Shape()[1] * outyolo.Shape()[2])
		for i := 0; i < outyolo.Shape()[0]; i++ {
			buf, _ := outyolo.At(i)
			yboxes32 = append(yboxes32, buf.(float32))
			buf, _ = rin.At(i)
			input32 = append(input32, buf.(float32))
		}
		break
	case Float64:
		rin.Reshape(in.Shape()[0] * in.Shape()[1] * in.Shape()[2])
		outyolo.Reshape(outyolo.Shape()[0] * outyolo.Shape()[1] * outyolo.Shape()[2])
		for i := 0; i < outyolo.Shape()[0]; i++ {
			buf, _ := outyolo.At(i)
			yboxes32 = append(yboxes32, float32(buf.(float64)))
			buf, _ = rin.At(i)
			input32 = append(input32, float32(buf.(float64)))
		}
		break
	default:
		panic("Unsupportable type for Yolo")
	}
	res := op.prepRT(input32, yboxes32, targets, grid)
	switch outyolo.Dtype() {
	case Float32:
		resten := tensor.New(tensor.WithShape(1, grid*grid*len(op.mask), 5+op.numClasses), tensor.Of(tensor.Float32), tensor.WithBacking(res))
		fmt.Println(resten)
		return resten, nil
	case Float64:
		res64 := make([]float64, len(res), len(res))
		for i := 0; i < len(res); i++ {
			res64[i] = float64(res[i])
		}
		resten := tensor.New(tensor.WithShape(1, grid*grid*len(op.mask), 5+op.numClasses), tensor.Of(tensor.Float64), tensor.WithBacking(res64))
		return resten, nil
	default:
		panic("Unsupportable type for Yolo")
	}
}

func (op *yoloOp) DiffWRT(inputs int) []bool { return []bool{true} }

//SymDiff -
func (op *yoloOp) SymDiff(inputs Nodes, output, grad *Node) (retVal Nodes, err error) {
	if err = checkArity(op, len(inputs)); err != nil {
		return
	}
	in := inputs[0]
	var op2 yoloOp
	op2 = *op
	diff := &yoloOpDiff{op2}

	var ret *Node
	if ret, err = ApplyOp(diff, in, grad); err != nil {
		return nil, err
	}
	return Nodes{ret}, nil

}
func (op *yoloOp) yoloDoer(in tensor.Tensor, batch, stride, grid, bboxAttrs, numAnchors int, currentAnchors []float64) (retVal tensor.Tensor, err error) {
	in.Reshape(batch, bboxAttrs*numAnchors, grid*grid)

	in.T(0, 2, 1)
	in.Transpose()
	in.Reshape(batch, grid*grid*numAnchors, bboxAttrs)

	// Activation of x, y, and objectness params
	sigmSlice(in.Slice(nil, nil, S(0, 2)))
	sigmSlice(in.Slice(nil, nil, S(4, 5+op.numClasses)))

	step := grid * numAnchors

	for ind := 0; ind < grid; ind++ {
		//View with the same Y coordinate (row)
		vy, err := in.Slice(nil, S(ind*step, ind*step+step), S(1))
		if err != nil {
			fmt.Println("1")
			panic(err)
		}
		switch in.Dtype() {
		case Float32:
			_, err = tensor.Add(vy, float32(ind), tensor.UseUnsafe())
			break
		case Float64:
			_, err = tensor.Add(vy, float64(ind), tensor.UseUnsafe())
			break
		default:
			panic("Unsupportable type for Yolo")
		}
		if err != nil {
			fmt.Println("2")
			panic(err)
		}

		//Tricky part
		for n := 0; n < numAnchors; n++ {
			//View with the same X coordinate (column)
			vx, err := in.Slice(nil, S(ind*numAnchors+n, in.Shape()[1], step), S(0))
			if err != nil {
				fmt.Println("3")
				panic(err)
			}
			switch in.Dtype() {
			case Float32:
				_, err = tensor.Add(vx, float32(ind), tensor.UseUnsafe())
				break
			case Float64:
				_, err = tensor.Add(vx, float64(ind), tensor.UseUnsafe())
				break
			default:
				panic("Unsupportable type for Yolo")
			}
			if err != nil {
				panic(err)
			}
		}

	}

	anchs := make([]float64, 0)
	for i := 0; i < grid*grid; i++ {
		anchs = append(anchs, currentAnchors...)
	}

	anch := tensor.New(
		tensor.Of(in.Dtype()),
		tensor.WithShape(1, grid*grid*numAnchors, 2),
	)
	for i := range anchs {
		switch in.Dtype() {
		case Float32:
			anch.Set(i, float32(anchs[i]))
			break
		case Float64:
			anch.Set(i, float64(anchs[i]))
		default:
			break
		}
	}
	fmt.Println(in.Dtype(), anch.Dtype())

	switch in.Dtype() {
	case Float32:
		_, err = tensor.Div(anch, float32(stride), tensor.UseUnsafe())
		if err != nil {
			fmt.Println("4")
			panic(err)
		}
		break
	case Float64:
		_, err = tensor.Div(anch, float64(stride), tensor.UseUnsafe())
		if err != nil {
			fmt.Println("5")
			panic(err)
		}
		break
	default:
		panic("Unsupportable type for Yolo")
	}
	if err != nil {
		fmt.Println("6")
		panic(err)
	}

	fmt.Println(anch.Dtype(), in.Dtype())

	vhw, err := in.Slice(nil, nil, S(2, 4))
	expSlice(vhw, err)
	// one := tensor.Ones(anch.Dtype(), vhw.Shape()...)

	_, err = tensor.Mul(vhw, anch, tensor.UseUnsafe())
	if err != nil {
		fmt.Println(vhw.Dtype(), anch.Dtype(), in.Dtype())
		fmt.Println("7")
		panic(err)
	}
	// fmt.Println(one)

	vv, err := in.Slice(nil, nil, S(0, 4))
	if err != nil {
		fmt.Println("8")
		panic(err)
	}

	switch in.Dtype() {
	case Float32:
		_, err = tensor.Mul(vv, float32(stride), tensor.UseUnsafe())
		break
	case Float64:
		_, err = tensor.Mul(vv, float64(stride), tensor.UseUnsafe())
		break
	default:
		panic("Unsupportable type for Yolo")
	}
	if err != nil {
		fmt.Println("9")
		panic(err)
	}
	return in, nil
}
func iou(r1, r2 image.Rectangle) float32 {
	intersection := r1.Intersect(r2)
	interArea := intersection.Dx() * intersection.Dy()
	r1Area := r1.Dx() * r1.Dy()
	r2Area := r2.Dx() * r2.Dy()
	return float32(interArea) / float32(r1Area+r2Area-interArea)
}

//returns best iou for all predictions and the number of target
func (op *yoloOp) prepBestIous(input, target []float32) [][]float32 {
	ious := make([][]float32, 0)
	imgsize := float32(op.inpDim)
	for i := 0; i < len(input); i = i + op.numClasses + 5 {
		ious = append(ious, []float32{0, -1})
		r1 := rectifyBox(input[i], input[i+1], input[i+2], input[i+3], op.inpDim)
		for j := 0; j < len(target); j = j + 5 {
			r2 := rectifyBox(target[j+1]*imgsize, target[j+2]*imgsize, target[j+3]*imgsize, target[j+4]*imgsize, op.inpDim)
			curiou := iou(r1, r2)
			if curiou > ious[i/(5+op.numClasses)][0] {
				ious[i/(5+op.numClasses)][0] = curiou
				ious[i/(5+op.numClasses)][1] = float32(j / 5)
			}
		}
	}
	return ious
}

//returns -1 if best anchor is not in the mask, else returns num of box with its coords; correct for yolov3 only!;
func (op *yoloOp) prepBestAnchors(target []float32, gridSize float32) [][]int {
	bestAnchors := make([][]int, len(target)/5, len(target)/5)
	imgsize := float32(op.inpDim)
	for j := 0; j < len(target); j = j + 5 {
		r2 := rectifyBox(0, 0, target[j+3]*imgsize, target[j+4]*imgsize, op.inpDim) //not absolutely confident in rectangle sizes
		var bestiou float32
		bestiou = 0.0
		bestAnchors[j/5] = make([]int, 3)
		for i := 0; i < len(op.anchors); i = i + 2 {
			r1 := rectifyBox(0, 0, float32(op.anchors[i]), float32(op.anchors[i+1]), op.inpDim)
			curiou := iou(r1, r2)
			if curiou >= bestiou {
				bestAnchors[j/5][0] = i
				bestiou = curiou
			}
		}
		bestAnchors[j/5][0] = indexInt(op.mask, bestAnchors[j/5][0]/2)
		if bestAnchors[j/5][0] != -1 {
			bestAnchors[j/5][1] = int(math.Floor(float64(target[j+1] * gridSize)))
			bestAnchors[j/5][2] = int(math.Floor(float64(target[j+2] * gridSize)))
		}
	}
	return bestAnchors
}

//returns array with values, that yolo layer should have
func (op *yoloOp) prepRT(input, yoloBoxes, target []float32, gridSize int) []float32 {
	rt := make([]float32, len(yoloBoxes), len(yoloBoxes))
	gsf32 := float32(gridSize)
	bestAnchors := op.prepBestAnchors(target, float32(gridSize))
	bestIous := op.prepBestIous(yoloBoxes, target)
	for i := 0; i < len(yoloBoxes); i = i + (5 + op.numClasses) {
		if bestIous[i/(5+op.numClasses)][0] <= float32(op.ignoreTresh) {
			//rt[i+4] = bceLoss(0, yoloBoxes[i+4])
			rt[i+4] = input[i+4] + 14
		}
	}
	for i := 0; i < len(bestAnchors); i++ {
		if bestAnchors[i][0] != -1 {
			scale := (2 - target[i*5+3]*target[i*5+4])

			gi := bestAnchors[i][1]
			gj := bestAnchors[i][2]
			gx := unsigm(target[i*5+1]*gsf32 - float32(gi))
			gy := unsigm(target[i*5+2]*gsf32 - float32(gj))
			banchor := op.mask[bestAnchors[i][0]] * 2
			gw := float32(math.Log(float64(target[i*5+3])/op.anchors[banchor] + 1e-16))
			gh := float32(math.Log(float64(target[i*5+4])/op.anchors[banchor+1] + 1e-16))
			fmt.Println(bestAnchors[i], gi, gj, gx, gy, gw, gh, scale)
			boxi := gj*gridSize*len(op.mask) + gi*len(op.mask) + bestAnchors[i][0]

			rt[boxi] = scale * (input[boxi] - gx)     //mseLoss(gx, input[boxi], scale)
			rt[boxi+1] = scale * (input[boxi+1] - gy) //mseLoss(gy, input[boxi+1], scale)
			rt[boxi+2] = scale * (input[boxi+2] - gw) //mseLoss(gw, input[boxi+2], scale)
			rt[boxi+3] = scale * (input[boxi+3] - gh) //mseLoss(gh, input[boxi+3], scale)
			rt[boxi+4] = input[boxi+4] - 14
			for j := 4; j < 5+op.numClasses; j++ {
				rt[boxi+j] = input[boxi+j]
				if j == int(target[i]) {
					rt[boxi+j] = input[boxi+j] - 14
				} else {
					rt[boxi+j] = input[boxi+j] + 14
				}
			}
			/*
				rt[boxi] = mseLoss(gx, input[boxi], scale)
				rt[boxi+1] = mseLoss(gy, input[boxi+1], scale)
				rt[boxi+2] = mseLoss(gw, input[boxi+2], scale)
				rt[boxi+3] = mseLoss(gh, input[boxi+3], scale)
				rt[boxi+4] = bceLoss(1, yoloBoxes[boxi+4])
				for j := 0; j < op.numClasses; j++ {
					if j == int(target[i]) {
						rt[boxi+5+j] = bceLoss(1, yoloBoxes[boxi+4])
					} else {
						rt[boxi+5+j] = bceLoss(0, yoloBoxes[boxi+4])
					}
				}*/
		}
	}
	for i := 0; i < len(rt); i++ {
		if rt[i] > 14 {
			rt[i] = 14
		}
		if rt[i] < (-14) {
			rt[i] = -14
		}
	}
	return rt
}
func indexInt(arr []int, k int) int {
	for i, j := range arr {
		if j == k {
			return i
		}
	}
	return -1
}
func rectifyBox(x, y, h, w float32, imgsize int) image.Rectangle {
	return image.Rect(max(int(x-w/2), 0), max(int(y-h/2), 0), min(int(x+w/2+1), imgsize), min(int(y+h/2+1), imgsize))
}
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

//with min value of log=-35
func bceLoss(target, pred float32) float32 {
	if target == 1.0 {

		return -float32(math.Log(float64(pred) + 1e-16))
	}
	return -float32(math.Log((1.0 - float64(pred)) + 1e-16))
}
func mseLoss(target, pred, scale float32) float32 {
	return float32(math.Pow(float64(scale*(target-pred)), 2)) / 2.0
}
func unsigm(target float32) float32 {
	p1 := math.Log(float64(1-target) + 1e-6)
	p2 := math.Log(float64(target) + 1e-6)
	p3 := p2 - p1

	if p3 > 100 {
		return 100.0
	}
	if p3 < (-100) {
		return -100
	}
	return float32(-p1 + p2)
}

type yoloOpDiff struct {
	yoloOp
}

func (op *yoloOpDiff) Arity() int { return 2 }
func (op *yoloOpDiff) Type() hm.Type {
	a := hm.TypeVariable('a')
	t := newTensorType(4, a)
	o := newTensorType(3, a)
	return hm.NewFnType(t, o, t)
}
func (op *yoloOpDiff) ReturnsPtr() bool     { return true }
func (op *yoloOpDiff) CallsExtern() bool    { return false }
func (op *yoloOpDiff) OverwritesInput() int { return -1 }
func (op *yoloOpDiff) InferShape(inputs ...DimSizer) (tensor.Shape, error) {
	s := inputs[0].(tensor.Shape).Clone()
	return s, nil
}
func (op *yoloOp) DoDiff(ctx ExecutionContext, inputs Nodes, output *Node) (err error) {
	panic("yoloOp.DoDiff")
	if err = checkArity(op, len(inputs)); err != nil {
		return
	}
	input := inputs[0]
	inputDV, outDV := getDV(input, output)
	inGrad := inputDV.d
	outValue := outDV.Value
	switch input.Dtype() {
	case tensor.Float32:
		inGradData := inGrad.Data().([]float32)
		outValueData := outValue.Data().([]float32)
		for i := range inGradData {
			inGradData[i] = 0.0
		}
		for i := 0; i < len(outValueData); i = i + 5 + op.numClasses {
			for j := 0; j < 4; j++ {
				inGradData[i+j] = outValueData[i+j]
			}
			for j := 4; j < 5+op.numClasses; j++ {
				inGradData[i+j] = 0 * outValueData[i+j] * (1.0 - outValueData[i+j])
			}
		}
	case tensor.Float64:
		inGradData := inGrad.Data().([]float64)
		outValueData := outValue.Data().([]float64)
		for i := range inGradData {
			inGradData[i] = 0.0
		}
		for i := 0; i < len(outValueData); i = i + 5 + op.numClasses {
			for j := 0; j < 4; j++ {
				inGradData[i+j] = outValueData[i+j]
			}
			for j := 4; j < 5+op.numClasses; j++ {
				inGradData[i+j] = outValueData[i+j] * (1.0 - outValueData[i+j])
			}
		}
	}
	return
}
func (op *yoloOpDiff) Do(inputs ...Value) (Value, error) {
	in := inputs[0]
	output := inputs[1]

	inGrad := tensor.New(tensor.Of(in.Dtype()), tensor.WithShape(in.Shape().Clone()...), tensor.WithEngine(in.(tensor.Tensor).Engine()))
	switch in.Dtype() {
	case tensor.Float32:
		inGradData := inGrad.Data().([]float32)
		outValueData := output.Data().([]float32)
		for i := range inGradData {
			inGradData[i] = outValueData[i] //0.0
		}
		inGrad.Reshape(1, len(op.mask)*(5+op.numClasses), op.gridSize, op.gridSize)
		return inGrad, nil
		for i := 0; i < len(outValueData); i = i + 5 + op.numClasses {
			for j := 0; j < 4; j++ {
				inGradData[i+j] = outValueData[i+j] // float32(len(outValueData))
			}
			for j := 4; j < 5+op.numClasses; j++ {
				inGradData[i+j] = outValueData[i+j] // float32(len(outValueData)) //* (1.0 - outValueData[i+j])
			}
		}
	case tensor.Float64:
		inGradData := inGrad.Data().([]float64)
		outValueData := output.Data().([]float64)
		for i := range inGradData {
			inGradData[i] = 0.0
		}
		for i := 0; i < len(outValueData); i = i + 5 + op.numClasses {
			for j := 0; j < 4; j++ {
				inGradData[i+j] = outValueData[i+j]
			}
			for j := 4; j < 5+op.numClasses; j++ {
				inGradData[i+j] = outValueData[i+j] * (1.0 - outValueData[i+j])
			}
		}
	}
	inGrad.Reshape(1, len(op.mask)*(5+op.numClasses), op.gridSize, op.gridSize)
	return inGrad, nil
}